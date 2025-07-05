package dispatcher

import (
	"context"
	"errors"
	"sync"
	"time"

	_ "github.com/lib/pq"

	"github.com/ludusrusso/kannon/internal/mailbuilder"
	"github.com/ludusrusso/kannon/internal/pool"
	"github.com/ludusrusso/kannon/internal/runner"
	"github.com/ludusrusso/kannon/internal/statssec"
	"github.com/ludusrusso/kannon/internal/x/container"
	"github.com/sirupsen/logrus"

	"github.com/nats-io/nats.go"
)

func Run(ctx context.Context, cnt *container.Container) error {
	log := logrus.WithField("component", "dispatcher")

	log.Info("🚀 Starting dispatcher")

	q := cnt.Queries()

	ss := statssec.NewStatsService(q)
	pm := pool.NewSendingPoolManager(q)
	mb := mailbuilder.NewMailBuilder(q, ss)

	nc := cnt.Nats()
	js := cnt.NatsJetStream()
	mustConfigureJS(js)

	d := disp{
		ss:  ss,
		pm:  pm,
		mb:  mb,
		pub: nc,
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
			logrus.Fatalf("error in runner, %v", err)
		}
		wg.Done()
	}()

	wg.Wait()

	return nil
}

func mustConfigureJS(js nats.JetStreamContext) {
	confs := nats.StreamConfig{
		Name:        "kannon-sending",
		Description: "Email Sending Pool for Kannon",
		Replicas:    1,
		Subjects:    []string{"kannon.sending"},
		Retention:   nats.LimitsPolicy,
		Duplicates:  10 * time.Minute,
		MaxAge:      24 * time.Hour,
		Storage:     nats.FileStorage,
		Discard:     nats.DiscardOld,
	}
	info, err := js.AddStream(&confs)
	if errors.Is(err, nats.ErrStreamNameAlreadyInUse) {
		logrus.Infof("stream exists")
	} else if err != nil {
		logrus.Fatalf("cannot create js stream: %v", err)
	}
	logrus.Infof("created js stream: %v", info)
}
