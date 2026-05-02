package dispatcher

import (
	"context"
	"fmt"
	"time"

	"github.com/kannon-email/kannon/internal/batch"
	"github.com/kannon-email/kannon/internal/envelope"
	"github.com/kannon-email/kannon/internal/pool"
	"github.com/kannon-email/kannon/internal/publisher"
	"github.com/kannon-email/kannon/internal/statssec"
	"github.com/kannon-email/kannon/internal/utils"
	statstypes "github.com/kannon-email/kannon/proto/kannon/stats/types"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/sirupsen/logrus"
	"google.golang.org/protobuf/proto"
)

type disp struct {
	ss      statssec.StatsService
	claimer pool.Claimer
	eb      envelope.Builder
	pub     publisher.Publisher
	js      jetstream.JetStream
}

func (d *disp) log() *logrus.Entry {
	return logrus.WithField("component", "dispatcher")
}

func (d *disp) DispatchCycle(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	emails, err := d.claimer.ClaimForDispatch(ctx, 20)
	if err != nil {
		return fmt.Errorf("cannot prepare emails for send: %v", err)
	}

	d.log().Debugf("seding %d emails", len(emails))

	for _, dlv := range emails {
		log := d.log()
		env, err := d.eb.Build(ctx, dlv)

		if err != nil {
			log.WithError(err).Errorf("Cannot send email")
			continue
		}

		log = log.WithField("email", utils.ObfuscateEmail(env.To())).WithField("email_id", env.EmailID())

		if err := publisher.SendEmail(d.pub, env); err != nil {
			log.WithError(err).Errorf("Cannot send email")
			continue
		}

		log.Infof("[✅ accepted]")
	}

	d.log().Debugf("done sending emails")
	return nil
}

func (d *disp) handleErrors(ctx context.Context) error {
	sbj := "kannon.stats.error"
	subName := "dispatcher-error"
	return d.handleMsg(ctx, sbj, subName, d.parseErrorsFunc)
}

func (d *disp) parseErrorsFunc(ctx context.Context, m *statstypes.Stats) error {
	bounceErr := m.Data.GetError()
	if bounceErr == nil {
		return fmt.Errorf("stats is not of type error")
	}

	dlv, err := d.claimer.Lookup(ctx, batch.ID(m.MessageId), m.Email)
	if err != nil {
		return fmt.Errorf("cannot lookup delivery: %w", err)
	}
	if err := d.claimer.Reschedule(ctx, dlv); err != nil {
		return fmt.Errorf("cannot set delivered: %w", err)
	}
	return nil
}

func (d *disp) handleDelivers(ctx context.Context) error {
	sbj := "kannon.stats.delivered"
	subName := "dispatcher-delivered"
	return d.handleMsg(ctx, sbj, subName, d.parsDeliveredFunc)
}

func (d *disp) parsDeliveredFunc(ctx context.Context, m *statstypes.Stats) error {
	dlv, err := d.claimer.Lookup(ctx, batch.ID(m.MessageId), m.Email)
	if err != nil {
		return fmt.Errorf("cannot lookup delivery: %w", err)
	}
	if err := d.claimer.Drop(ctx, dlv); err != nil {
		return fmt.Errorf("cannot set delivered: %w", err)
	}
	return nil
}

func (d *disp) handleBounced(ctx context.Context) error {
	sbj := "kannon.stats.bounced"
	subName := "dispatcher-bounced"
	return d.handleMsg(ctx, sbj, subName, d.parsBouncedFunc)
}

func (d *disp) parsBouncedFunc(ctx context.Context, m *statstypes.Stats) error {
	dlv, err := d.claimer.Lookup(ctx, batch.ID(m.MessageId), m.Email)
	if err != nil {
		return fmt.Errorf("cannot lookup delivery: %w", err)
	}
	if err := d.claimer.Drop(ctx, dlv); err != nil {
		return fmt.Errorf("cannot set delivered: %w", err)
	}

	return nil
}

type parseFunc func(ctx context.Context, msg *statstypes.Stats) error

func (d *disp) handleMsg(ctx context.Context, sbj, subName string, parse parseFunc) error {
	con := utils.MustGetPullSubscriber(ctx, d.js, "kannon-stats", sbj, subName)
	c, err := con.Consume(func(msg jetstream.Msg) {
		d.handleWithAck(ctx, msg, func(ctx context.Context, msg jetstream.Msg) error {
			m := &statstypes.Stats{}
			if err := proto.Unmarshal(msg.Data(), m); err != nil {
				return err
			}
			return parse(ctx, m)
		})
	})
	if err != nil {
		return fmt.Errorf("cannot consume %s for %s: %w", sbj, subName, err)
	}
	defer c.Drain()

	<-ctx.Done()
	d.log().Infof("Consumer %s stopped", subName)
	return ctx.Err()
}

func (d *disp) handleWithAck(ctx context.Context, msg jetstream.Msg, f func(ctx context.Context, msg jetstream.Msg) error) {
	err := f(ctx, msg)
	if err != nil {
		if err := msg.Nak(); err != nil {
			d.log().Errorf("Cannot nak msg to nats: %v", err)
		}
	} else {
		if err := msg.Ack(); err != nil {
			d.log().Errorf("Cannot hack msg to nats: %v", err)
		}
	}
}
