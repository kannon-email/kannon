package dispatcher

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"golang.org/x/sync/errgroup"

	sqlc "github.com/kannon-email/kannon/internal/db"
	"github.com/kannon-email/kannon/internal/envelope"
	"github.com/kannon-email/kannon/internal/pool"
	"github.com/kannon-email/kannon/internal/runner"
	"github.com/kannon-email/kannon/internal/statssec"
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
	claimer := pool.NewClaimer(sqlc.NewDeliveryRepository(cnt.DB(), cnt.BackoffPolicy()))
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

	return eg.Wait()
}

// streamCreator is the minimal seam over jetstream.JetStream needed to
// create/update the sending stream. jetstream.JetStream satisfies it
// structurally, so production code passes one straight through; tests can
// supply a fake to exercise the retry/backoff loop without a real NATS
// connection.
type streamCreator interface {
	CreateOrUpdateStream(ctx context.Context, cfg jetstream.StreamConfig) (jetstream.Stream, error)
}

// configureStreamAttempts and configureStreamBaseDelay bound the retry loop
// in retryConfigureSendingStream: a handful of attempts with a doubling
// delay is enough to ride out a NATS instance that is briefly slow to come
// up (#365) without turning a longer outage into an indefinite hang.
const (
	configureStreamAttempts  = 5
	configureStreamBaseDelay = 1 * time.Second
)

// configureSendingStream ensures the kannon-sending stream exists.
//
// It used to os.Exit(1) on the first JetStream error. In Kubernetes, a NATS
// pod that is briefly slow to come up made the dispatcher crash-loop, which
// only amplified the load on NATS. Errors are now returned with retries so
// the caller decides whether to abort.
func configureSendingStream(ctx context.Context, js jetstream.JetStream) error {
	return retryConfigureSendingStream(ctx, js, configureStreamAttempts, configureStreamBaseDelay)
}

// retryConfigureSendingStream retries sc.CreateOrUpdateStream up to attempts
// times, waiting baseDelay*2^i between attempt i and i+1. It honours ctx
// cancellation while waiting instead of sleeping blindly, so a shutdown
// during startup returns promptly rather than burning the rest of the
// backoff.
func retryConfigureSendingStream(ctx context.Context, sc streamCreator, attempts int, baseDelay time.Duration) error {
	name := "kannon-sending"
	confs := jetstream.StreamConfig{
		Name:        name,
		Description: "Email Sending Pool for Kannon",
		Replicas:    1,
		Subjects:    []string{"kannon.sending"},
		Retention:   jetstream.LimitsPolicy,
		Duplicates:  10 * time.Minute,
		MaxAge:      24 * time.Hour,
		Storage:     jetstream.FileStorage,
		Discard:     jetstream.DiscardOld,
	}

	var lastErr error
	for attempt := range attempts {
		_, err := sc.CreateOrUpdateStream(ctx, confs)
		if err == nil {
			slog.Info(fmt.Sprintf("created js stream: %v", name))
			return nil
		}
		lastErr = err

		if attempt == attempts-1 {
			break
		}

		delay := baseDelay * time.Duration(1<<attempt)
		slog.Warn("cannot create js stream, retrying", "err", err, "attempt", attempt+1, "delay", delay)
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(delay):
		}
	}

	return fmt.Errorf("cannot create js stream after %d attempts: %w", attempts, lastErr)
}
