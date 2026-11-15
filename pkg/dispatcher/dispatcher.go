package dispatcher

import (
	"context"
	"time"

	"golang.org/x/sync/errgroup"

	sqlc "github.com/kannon-email/kannon/internal/db"
	"github.com/kannon-email/kannon/internal/envelope"
	"github.com/kannon-email/kannon/internal/mailflow"
	"github.com/kannon-email/kannon/internal/pool"
	"github.com/kannon-email/kannon/internal/returnpath"
	"github.com/kannon-email/kannon/internal/runner"
	"github.com/kannon-email/kannon/internal/statssec"
	"github.com/kannon-email/kannon/x/container"
)

// New constructs the dispatcher runnable. Its one setting is the bounce
// secret it signs return paths with, which the boot path has already required.
func New(cnt *container.Container) container.Runnable {
	rp := returnpath.MustLoad()
	return container.Runnable{
		Name: "dispatcher",
		Run: func(ctx context.Context, ready func()) error {
			return run(ctx, cnt, rp, ready)
		},
	}
}

func run(ctx context.Context, cnt *container.Container, rp returnpath.Signer, ready func()) error {
	q := cnt.Queries()

	ss := statssec.NewStatsService(q)
	claimer := pool.NewClaimer(sqlc.NewDeliveryRepository(cnt.DB(), cnt.BackoffPolicy(), cnt.RetryWindow()))
	// An Envelope travels as one NATS message, so the server's limit for one is
	// the most content a Delivery can be built with.
	eb := envelope.NewBuilder(q, ss, rp, int(cnt.Nats().MaxPayload()))

	js := cnt.NatsJetStream()

	d := disp{
		ss:      ss,
		claimer: claimer,
		eb:      eb,
		pub:     cnt.NatsPublisher(),
		js:      js,
		flow:    mailflow.New(cnt.Metrics()),
	}
	mailflow.RegisterPool(cnt.Metrics(), sqlc.NewPoolCensus(cnt.DB()))

	errored := d.consumer(ctx, erroredSubject, erroredConsumer)
	delivered := d.consumer(ctx, deliveredSubject, deliveredConsumer)
	bounced := d.consumer(ctx, bouncedSubject, bouncedConsumer)

	d.log().Info("🚀 Starting dispatcher")
	ready()

	eg, ctx := errgroup.WithContext(ctx)

	eg.Go(func() error {
		return d.handleErrors(ctx, errored)
	})

	eg.Go(func() error {
		return d.handleDelivers(ctx, delivered)
	})

	eg.Go(func() error {
		return d.handleBounced(ctx, bounced)
	})

	eg.Go(func() error {
		return runner.Run(ctx, d.DispatchCycle, runner.WaitLoop(1*time.Second))
	})

	eg.Go(func() error {
		return runner.Run(ctx, d.reclaimLoop, runner.WaitLoop(pool.ReclaimInterval))
	})

	return eg.Wait()
}
