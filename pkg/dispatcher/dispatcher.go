package dispatcher

import (
	"context"
	"sync"
	"time"

	_ "github.com/lib/pq"

	"github.com/kannon-email/kannon/internal/mailbuilder"
	"github.com/kannon-email/kannon/internal/pool"
	"github.com/kannon-email/kannon/internal/runner"
	"github.com/kannon-email/kannon/internal/statssec"
	"github.com/kannon-email/kannon/internal/x/container"
	"github.com/sirupsen/logrus"

	"github.com/nats-io/nats.go/jetstream"
)

func Run(ctx context.Context, cnt *container.Container) error {
	log := logrus.WithField("component", "dispatcher")

	log.Info("🚀 Starting dispatcher")

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
		log: log,
	}

	var wg sync.WaitGroup

	wg.Add(1)
	go func() {
		d.handleErrors(ctx)
		wg.Done()
	}()

	wg.Add(1)
	go func() {
		d.handleDelivers(ctx)
		wg.Done()
	}()

	wg.Add(1)
	go func() {
		d.handleBounced(ctx)
		wg.Done()
	}()

	wg.Add(1)
	go func() {
		err := runner.Run(ctx, d.DispatchCycle, runner.WaitLoop(1*time.Second))
		if err != nil {
			logrus.Errorf("error in runner, %v", err)
		}
		wg.Done()
	}()

	wg.Wait()

	return nil
}

func mustConfigureSendingStream(ctx context.Context, js jetstream.JetStream) {
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
	info, err := js.CreateOrUpdateStream(ctx, confs)
	if err != nil {
		logrus.Fatalf("cannot create js stream: %v", err)
	}
	logrus.Infof("created js stream: %v", info.Config.Name)
}
