package dispatcher

import (
	"context"
	"fmt"
	"time"

	"github.com/ludusrusso/kannon/internal/mailbuilder"
	"github.com/ludusrusso/kannon/internal/pool"
	"github.com/ludusrusso/kannon/internal/publisher"
	"github.com/ludusrusso/kannon/internal/statssec"
	"github.com/ludusrusso/kannon/internal/utils"
	statstypes "github.com/ludusrusso/kannon/proto/kannon/stats/types"
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
}

func (d disp) DispatchCycle(pctx context.Context) error {
	ctx, cancel := context.WithTimeout(pctx, 10*time.Second)
	defer cancel()
	emails, err := d.pm.PrepareForSend(ctx, 100)
	if err != nil {
		return fmt.Errorf("cannot prepare emails for send: %v", err)
	}
	for _, email := range emails {
		data, err := d.mb.BuildEmail(ctx, email)
		if err != nil {
			logrus.Errorf("Cannot send email %v: %v", email.Email, err)
			continue
		}
		if err := publisher.SendEmail(d.pub, data); err != nil {
			logrus.Errorf("Cannot send email %v: %v", email.Email, err)
			continue
		}
		logrus.Infof("[✅ accepted]: %v %v", data.To, data.EmailId)
	}
	logrus.Debugf("done sending emails")
	return nil
}

func (d disp) handleErrors(ctx context.Context) {
	sbj := "kannon.stats.error"
	subName := "dispatcher-error"
	handleMsg(ctx, d.js, sbj, subName, d.parseErrorsFunc)
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

func (d disp) handleDelivereds(ctx context.Context) {
	sbj := "kannon.stats.delivered"
	subName := "dispatcher-delivered"
	handleMsg(ctx, d.js, sbj, subName, d.parsDeliveredFunc)
}

func (d disp) parsDeliveredFunc(ctx context.Context, m *statstypes.Stats) error {
	if err := d.pm.CleanEmail(ctx, m.MessageId, m.Email); err != nil {
		return fmt.Errorf("cannot set delivered: %w", err)
	}
	return nil
}

func (d disp) handleRejected(ctx context.Context) {
	sbj := "kannon.stats.rejected"
	subName := "dispatcher-rejected"
	handleMsg(ctx, d.js, sbj, subName, d.parsRejectedFunc)
}

func (d disp) parsRejectedFunc(ctx context.Context, m *statstypes.Stats) error {
	if err := d.pm.CleanEmail(ctx, m.MessageId, m.Email); err != nil {
		return fmt.Errorf("cannot set delivered: %w", err)
	}

	return nil
}

func (d disp) handleBounced(ctx context.Context) {
	sbj := "kannon.stats.bounced"
	subName := "dispatcher-bounced"
	handleMsg(ctx, d.js, sbj, subName, d.parsBouncedFunc)
}

func (d disp) parsBouncedFunc(ctx context.Context, m *statstypes.Stats) error {
	if err := d.pm.CleanEmail(ctx, m.MessageId, m.Email); err != nil {
		return fmt.Errorf("cannot set delivered: %w", err)
	}

	return nil
}

type parseFunc func(ctx context.Context, msg *statstypes.Stats) error

func handleMsg(ctx context.Context, js nats.JetStreamContext, sbj, subName string, parse parseFunc) {
	con := utils.MustGetPullSubscriber(js, sbj, subName)
	for {
		msgs, err := con.Fetch(10, nats.MaxWait(10*time.Second))
		if err != nil {
			if err != nats.ErrTimeout {
				logrus.Errorf("error fetching messages: %v", err)
			}
			continue
		}
		for _, msg := range msgs {
			m := &statstypes.Stats{}
			if err := proto.Unmarshal(msg.Data, m); err != nil {
				handleAck(msg, err)
				continue
			}
			err := parse(ctx, m)
			handleAck(msg, err)
		}
	}
}

func handleAck(msg *nats.Msg, err error) {
	if err != nil {
		if err := msg.Nak(); err != nil {
			logrus.Errorf("Cannot nak msg to nats: %v", err)
		}
	} else {
		if err := msg.Ack(); err != nil {
			logrus.Errorf("Cannot hack msg to nats: %v", err)
		}
	}
}
