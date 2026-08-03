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
	"google.golang.org/protobuf/types/known/timestamppb"
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

// Dispatch timeouts. The claim gets its own budget; each Delivery then
// gets an individual budget for Build. A single per-cycle budget shared
// across the whole claimed page guillotines the page tail when one
// Delivery is slow — see #400.
const (
	claimTimeout       = 10 * time.Second
	perDeliveryTimeout = 5 * time.Second
)

// The reason carried by the Failed stat of a Delivery whose Retry Budget ran
// out. It states what ran out and on which leg, and nothing else: this string is
// customer-visible through the stats API, so it may never carry a raw error
// string, an email address, or anything about the database.
const (
	// reasonBudgetSpentDispatching: no Envelope for this Delivery could be built
	// or handed on before the budget ran out — its Batch's Template is gone, its
	// Domain's DKIM key is unusable, or NATS would not take the Envelope.
	reasonBudgetSpentDispatching = "retry budget exhausted while dispatching"

	// reasonBudgetSpentSending: the budget ran out with a transmission attempt
	// outstanding.
	reasonBudgetSpentSending = "retry budget exhausted while sending"
)

func (d *disp) DispatchCycle(ctx context.Context) error {
	emails, err := d.claimForDispatch(ctx)
	if err != nil {
		return fmt.Errorf("cannot prepare emails for send: %w", err)
	}

	d.log().Debug(fmt.Sprintf("seding %d emails", len(emails)))

	for _, dlv := range emails {
		d.dispatchOne(ctx, dlv)
	}

	d.log().Debug("done sending emails")
	return nil
}

func (d *disp) claimForDispatch(ctx context.Context) ([]*delivery.Delivery, error) {
	ctx, cancel := context.WithTimeout(ctx, claimTimeout)
	defer cancel()
	return d.claimer.ClaimForDispatch(ctx, 20)
}

// dispatchOne builds and publishes the Envelope for one claimed Delivery.
// On any failure the Delivery goes back through the retry chokepoint: the
// claim already flipped it to 'sending', and the only other exits from
// that status are stats-feedback driven — which can never arrive for an
// Envelope that was never published. Dropping the row on the floor here
// loses it silently and permanently (#400), so it is either handed back to
// the pool or ended as Failed, never neither.
func (d *disp) dispatchOne(ctx context.Context, dlv *delivery.Delivery) {
	log := d.log().With(
		"email", utils.ObfuscateEmail(dlv.Email()),
		"batch_id", dlv.BatchID().String(),
	)

	buildCtx, cancel := context.WithTimeout(ctx, perDeliveryTimeout)
	defer cancel()

	env, err := d.eb.Build(buildCtx, dlv)
	if err != nil {
		log.With("err", err).Error("Cannot send email")
		d.reschedule(ctx, dlv, log)
		return
	}

	log = log.With("email_id", env.EmailID())

	if err := publisher.SendEmail(d.pub, env); err != nil {
		log.With("err", err).Error("Cannot send email")
		d.reschedule(ctx, dlv, log)
		return
	}

	log.Info("[✅ accepted]")
}

// reschedule is dispatchOne's side of the chokepoint. It logs what retryOrFail
// reports instead of returning it, because a dispatch cycle handles each
// Delivery's failure in-loop and must carry on with the rest of the claimed page
// (#400) — whereas the error-stat consumer needs the error to drive Nak/Ack.
func (d *disp) reschedule(ctx context.Context, dlv *delivery.Delivery, log *slog.Logger) {
	if err := d.retryOrFail(ctx, dlv, reasonBudgetSpentDispatching); err != nil {
		log.With("err", err).Error("Cannot hand delivery back to the pool after dispatch failure")
	}
}

// retryOrFail hands a Delivery back to the Pool for another attempt, or ends it
// as Failed when its Retry Budget has no room for one.
//
// This is the single chokepoint for both paths that return a Delivery to the
// Pool — the Dispatcher's own dispatch failure and the error-stat consumer — so
// the give-up decision cannot drift between them. One predicate on the entity
// governs every retry in the system (ADR 0007).
//
// It runs on a context detached from cancellation: the failure being handled is
// often the cycle context dying, and neither the recovery write nor the terminal
// one may share that fate.
//
// A reader will trip on Failed being emitted here rather than Bounced, since
// CONTEXT.md (*Retry Budget*) says an exhausted Delivery is Bounced when the
// attempt that ran out the clock was answered and Failed when none ever was —
// and the error-stat leg has an SMTP reply in hand. The two agree, because a
// Delivery whose ShouldRetry is already false never reaches this leg: the
// SMTPSender bounces it itself (handleSendError). The only way a spent budget
// arrives here is that the attempt tally advanced through an *unanswered*
// internal event — a dispatch failure, or a reclaim of a stranded row — after the
// Envelope was built, and it is that event, not the answered one, that ran out
// the clock.
func (d *disp) retryOrFail(ctx context.Context, dlv *delivery.Delivery, reason string) error {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), claimTimeout)
	defer cancel()

	if dlv.CanRetry() {
		return d.claimer.Reschedule(ctx, dlv)
	}
	return d.fail(ctx, dlv, reason)
}

// fail ends a Delivery whose Retry Budget is spent: the sender is told, and the
// row leaves the Pool.
//
// The stat is published before the row is dropped. If the publish fails the row
// stays in an in-flight status and the next reclaim brings it back for this same
// verdict, at worst emitting the outcome twice — stats accumulate per Delivery
// and the latest one wins, so a duplicate is harmless. The other order trades
// that for the bug being fixed here: a Delivery that vanishes with `accepted` as
// its last word.
func (d *disp) fail(ctx context.Context, dlv *delivery.Delivery, reason string) error {
	stat := &statstypes.Stats{
		MessageId: dlv.BatchID().String(),
		Domain:    dlv.Domain(),
		Email:     dlv.Email(),
		Timestamp: timestamppb.Now(),
		Data: &statstypes.StatsData{
			Data: &statstypes.StatsData_Failed{
				Failed: &statstypes.StatsDataFailed{Reason: reason},
			},
		},
	}
	// PublishStat derives kannon.stats.failed from the payload, so the subject
	// cannot disagree with the outcome it carries.
	if err := publisher.PublishStat(d.pub, stat); err != nil {
		return fmt.Errorf("cannot publish failed stat: %w", err)
	}

	if err := d.claimer.Drop(ctx, dlv); err != nil {
		return fmt.Errorf("cannot drop delivery with a spent retry budget: %w", err)
	}

	d.log().Warn("[❌ failed] retry budget exhausted",
		"email", utils.ObfuscateEmail(dlv.Email()),
		"batch_id", dlv.BatchID().String(),
		"send_attempts", dlv.SendAttempts(),
		"reason", reason)
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
	if err := d.retryOrFail(ctx, dlv, reasonBudgetSpentSending); err != nil {
		return fmt.Errorf("cannot hand delivery back to the pool after a send error: %w", err)
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
