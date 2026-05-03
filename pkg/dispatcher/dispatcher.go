package dispatcher

import (
	"context"
	"time"

	_ "github.com/lib/pq"
	"golang.org/x/sync/errgroup"

	sqlc "github.com/kannon-email/kannon/internal/db"
	"github.com/kannon-email/kannon/internal/envelope"
	"github.com/kannon-email/kannon/internal/pool"
	"github.com/kannon-email/kannon/internal/runner"
	"github.com/kannon-email/kannon/internal/statssec"
	"github.com/kannon-email/kannon/x/container"
	"github.com/sirupsen/logrus"

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
	claimer := pool.NewClaimer(sqlc.NewDeliveryRepository(q, cnt.BackoffPolicy()))
	eb := envelope.NewBuilder(q, ss)

	js := cnt.NatsJetStream()
	mustConfigureSendingStream(ctx, js)

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

	return eg.Wait()
}

func mustConfigureSendingStream(ctx context.Context, js jetstream.JetStream) {
	name := "kannon-sending"
	confs := jetstream.StreamConfig{
		Name:        "kannon-sending",
		Description: "Email Sending Pool for Kannon",
		Replicas:    1,
		Subjects:    []string{"kannon.sending"},
		Retention:   jetstream.LimitsPolicy,
		Duplicates:  10 * time.Minute,
		MaxAge:      24 * time.Hour,
		Storage:     jetstream.FileStorage,
		Discard:     jetstream.DiscardOld,
	}
	_, err := js.CreateOrUpdateStream(ctx, confs)
	if err != nil {
		logrus.Fatalf("cannot create js stream: %v", err)
	}
	logrus.Infof("created js stream: %v", name)
}
