package dispatcher

import (
	"context"
	"time"

	_ "github.com/lib/pq"
	"golang.org/x/sync/errgroup"

	"github.com/kannon-email/kannon/internal/mailbuilder"
	"github.com/kannon-email/kannon/internal/pool"
	"github.com/kannon-email/kannon/internal/runner"
	"github.com/kannon-email/kannon/internal/statssec"
	"github.com/kannon-email/kannon/internal/x/container"
	"github.com/sirupsen/logrus"

	"github.com/nats-io/nats.go/jetstream"
)

func Run(ctx context.Context, cnt *container.Container) error {
	q := cnt.Queries()

	ss := statssec.NewStatsService(q)
	pm := pool.NewSendingPoolManager(q)
	mb := mailbuilder.NewMailBuilder(q, ss)

	js := cnt.NatsJetStream()
	mustConfigureSendingStream(ctx, js)

	d := disp{
		ss:  ss,
		pm:  pm,
		mb:  mb,
		pub: cnt.NatsPublisher(),
		js:  js,
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
