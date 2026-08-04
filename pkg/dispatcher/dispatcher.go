package dispatcher

import (
	"context"
	"fmt"
	"time"

	"golang.org/x/sync/errgroup"

	sqlc "github.com/kannon-email/kannon/internal/db"
	"github.com/kannon-email/kannon/internal/envelope"
	"github.com/kannon-email/kannon/internal/pool"
	"github.com/kannon-email/kannon/internal/runner"
	"github.com/kannon-email/kannon/internal/statssec"
	"github.com/kannon-email/kannon/internal/utils"
	"github.com/kannon-email/kannon/x/container"

	"github.com/nats-io/nats.go/jetstream"
)

// New constructs the dispatcher runnable. The dispatcher has no
// configurable knobs today, so it does not call container.LoadConfig.
func New(cnt *container.Container) container.Runnable {
	return container.Runnable{
		Name: "dispatcher",
		Run: func(ctx context.Context) error {
			return run(ctx, cnt)
		},
	}
}

func run(ctx context.Context, cnt *container.Container) error {
	q := cnt.Queries()

	ss := statssec.NewStatsService(q)
	claimer := pool.NewClaimer(sqlc.NewDeliveryRepository(cnt.DB(), cnt.BackoffPolicy(), cnt.RetryWindow()))
	eb := envelope.NewBuilder(q, ss)

	js := cnt.NatsJetStream()
	if err := configureSendingStream(ctx, js); err != nil {
		return fmt.Errorf("cannot configure sending stream: %w", err)
	}

	d := disp{
		ss:      ss,
		claimer: claimer,
		eb:      eb,
		pub:     cnt.NatsPublisher(),
		js:      js,
	}

	d.log().Info("🚀 Starting dispatcher")

	eg, ctx := errgroup.WithContext(ctx)

	eg.Go(func() error {
		return d.handleErrors(ctx)
	})

	eg.Go(func() error {
		return d.handleDelivers(ctx)
	})

	eg.Go(func() error {
		return d.handleBounced(ctx)
	})

	eg.Go(func() error {
		return runner.Run(ctx, d.DispatchCycle, runner.WaitLoop(1*time.Second))
	})

	eg.Go(func() error {
		return runner.Run(ctx, d.reclaimLoop, runner.WaitLoop(pool.ReclaimInterval))
	})

	return eg.Wait()
}

// configureSendingStream ensures the kannon-sending stream exists. This function holds the stream's
// configuration and nothing else: how hard to try when NATS is not answering yet belongs to
// utils.ConfigureStream, which every stream in Kannon goes through.
func configureSendingStream(ctx context.Context, js jetstream.JetStream) error {
	return utils.ConfigureStream(ctx, js, jetstream.StreamConfig{
		Name:        "kannon-sending",
		Description: "Email Sending Pool for Kannon",
		Replicas:    1,
		Subjects:    []string{"kannon.sending"},
		Retention:   jetstream.LimitsPolicy,
		Duplicates:  10 * time.Minute,
		MaxAge:      24 * time.Hour,
		Storage:     jetstream.FileStorage,
		Discard:     jetstream.DiscardOld,
	})
}
