package dispatcher

import (
	"context"
	"fmt"
	"time"

	"github.com/kannon-email/kannon/internal/mailbuilder"
	"github.com/kannon-email/kannon/internal/pool"
	"github.com/kannon-email/kannon/internal/publisher"
	"github.com/kannon-email/kannon/internal/statssec"
	"github.com/kannon-email/kannon/internal/utils"
	statstypes "github.com/kannon-email/kannon/proto/kannon/stats/types"
	"github.com/nats-io/nats.go"
	"github.com/sirupsen/logrus"
	"google.golang.org/protobuf/proto"
)

type disp struct {
	ss  statssec.StatsService
	pm  pool.SendingPoolManager
	mb  mailbuilder.MailBulder
	pub publisher.Publisher
	js  nats.JetStreamContext
	log *logrus.Entry
}

func (d disp) DispatchCycle(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	emails, err := d.pm.PrepareForSend(ctx, 20)
	if err != nil {
		return fmt.Errorf("cannot prepare emails for send: %v", err)
	}

	d.log.Debugf("[dispatcher] seding %d emails", len(emails))

	for _, email := range emails {
		data, err := d.mb.BuildEmail(ctx, email)
		if err != nil {
			d.log.Errorf("Cannot send email %v: %v", email.Email, err)
			continue
		}
		if err := publisher.SendEmail(d.pub, data); err != nil {
			d.log.Errorf("Cannot send email %v: %v", email.Email, err)
			continue
		}
		d.log.Infof("[✅ accepted]: %v %v", utils.ObfuscateEmail(data.To), data.EmailId)
	}

	d.log.Debugf("[dispatcher] done sending emails")
	return nil
}

func (d disp) handleErrors(ctx context.Context) {
	sbj := "kannon.stats.error"
	subName := "dispatcher-error"
	d.handleMsg(ctx, sbj, subName, d.parseErrorsFunc)
}

func (d disp) parseErrorsFunc(ctx context.Context, m *statstypes.Stats) error {
	bounceErr := m.Data.GetError()
	if bounceErr == nil {
		return fmt.Errorf("stats is not of type error")
	}

	if err := d.pm.RescheduleEmail(ctx, m.MessageId, m.Email); err != nil {
		return fmt.Errorf("cannot set delivered: %w", err)
	}
	return nil
}

func (d disp) handleDelivers(ctx context.Context) {
	sbj := "kannon.stats.delivered"
	subName := "dispatcher-delivered"
	d.handleMsg(ctx, sbj, subName, d.parsDeliveredFunc)
}

func (d disp) parsDeliveredFunc(ctx context.Context, m *statstypes.Stats) error {
	if err := d.pm.CleanEmail(ctx, m.MessageId, m.Email); err != nil {
		return fmt.Errorf("cannot set delivered: %w", err)
	}
	return nil
}

func (d disp) handleBounced(ctx context.Context) {
	sbj := "kannon.stats.bounced"
	subName := "dispatcher-bounced"
	d.handleMsg(ctx, sbj, subName, d.parsBouncedFunc)
}

func (d disp) parsBouncedFunc(ctx context.Context, m *statstypes.Stats) error {
	if err := d.pm.CleanEmail(ctx, m.MessageId, m.Email); err != nil {
		return fmt.Errorf("cannot set delivered: %w", err)
	}

	return nil
}

type parseFunc func(ctx context.Context, msg *statstypes.Stats) error

func (d disp) handleMsg(ctx context.Context, sbj, subName string, parse parseFunc) {
	con := utils.MustGetPullSubscriber(d.js, sbj, subName)
	for {
		msgs, err := con.Fetch(10, nats.MaxWait(10*time.Second))
		if err != nil {
			if err != nats.ErrTimeout {
				d.log.Errorf("error fetching messages: %v", err)
				return
			}
			continue
		}
		for _, msg := range msgs {
			m := &statstypes.Stats{}
			if err := proto.Unmarshal(msg.Data, m); err != nil {
				d.handleAck(msg, err)
				continue
			}
			err := parse(ctx, m)
			d.handleAck(msg, err)
		}
	}
}

func (d disp) handleAck(msg *nats.Msg, err error) {
	if err != nil {
		if err := msg.Nak(); err != nil {
			d.log.Errorf("Cannot nak msg to nats: %v", err)
		}
	} else {
		if err := msg.Ack(); err != nil {
			d.log.Errorf("Cannot hack msg to nats: %v", err)
		}
	}
}
