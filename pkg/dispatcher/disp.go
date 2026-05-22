package dispatcher

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/kannon-email/kannon/internal/batch"
	"github.com/kannon-email/kannon/internal/delivery"
	"github.com/kannon-email/kannon/internal/envelope"
	"github.com/kannon-email/kannon/internal/pool"
	"github.com/kannon-email/kannon/internal/publisher"
	"github.com/kannon-email/kannon/internal/statssec"
	"github.com/kannon-email/kannon/internal/utils"
	statstypes "github.com/kannon-email/kannon/proto/kannon/stats/types"
	"github.com/nats-io/nats.go/jetstream"
	"google.golang.org/protobuf/proto"
)

type disp struct {
	ss      statssec.StatsService
	claimer pool.Claimer
	eb      envelope.Builder
	pub     publisher.Publisher
	js      jetstream.JetStream
}

func (d *disp) log() *slog.Logger {
	return slog.With("component", "dispatcher")
}

func (d *disp) DispatchCycle(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	emails, err := d.claimer.ClaimForDispatch(ctx, 20)
	if err != nil {
		return fmt.Errorf("cannot prepare emails for send: %w", err)
	}

	d.log().Debug(fmt.Sprintf("seding %d emails", len(emails)))

	for _, dlv := range emails {
		log := d.log()
		env, err := d.eb.Build(ctx, dlv)

		if err != nil {
			log.With("err", err).Error("Cannot send email")
			continue
		}

		log = log.With("email", utils.ObfuscateEmail(env.To()), "email_id", env.EmailID())

		if err := publisher.SendEmail(d.pub, env); err != nil {
			log.With("err", err).Error("Cannot send email")
			continue
		}

		log.Info("[✅ accepted]")
	}

	d.log().Debug("done sending emails")
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
		return errors.New("stats is not of type error")
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
	d.log().Info(fmt.Sprintf("Consumer %s stopped", subName))
	return ctx.Err()
}

// nakDelay is the redelivery delay applied to transient stat-processing
// failures. Bare msg.Nak() triggers *instant* redelivery on the JetStream
// server, which turns any permanent error (e.g. lookup of an already-cleaned
// delivery) into a tight hot loop that pins a CPU. A delay caps the
// re-attempt rate even if MaxDeliver is misconfigured upstream.
const nakDelay = 5 * time.Second

func (d *disp) handleWithAck(ctx context.Context, msg jetstream.Msg, f func(ctx context.Context, msg jetstream.Msg) error) {
	err := f(ctx, msg)
	switch {
	case err == nil:
		if err := msg.Ack(); err != nil {
			d.log().Error("Cannot ack msg to nats", "err", err)
		}
	case errors.Is(err, delivery.ErrDeliveryNotFound):
		// Permanent: the delivery row is gone from the pool, so no amount of
		// redelivery will make Lookup succeed. Term the stat to keep the
		// consumer from spinning.
		d.log().Debug("dropping stat for unknown delivery", "err", err)
		if err := msg.Term(); err != nil {
			d.log().Error("Cannot term msg to nats", "err", err)
		}
	default:
		if err := msg.NakWithDelay(nakDelay); err != nil {
			d.log().Error("Cannot nak msg to nats", "err", err)
		}
	}
}
