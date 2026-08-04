package utils

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/nats-io/nats.go/jetstream"
)

// StreamCreator is the minimal seam over jetstream.JetStream needed to create or update a stream.
// jetstream.JetStream satisfies it structurally, so production code passes one straight through;
// a test can supply a fake and exercise the retry loop below without a NATS connection at all.
type StreamCreator interface {
	CreateOrUpdateStream(ctx context.Context, cfg jetstream.StreamConfig) (jetstream.Stream, error)
}

// configureStreamAttempts and configureStreamBaseDelay bound the retry loop: a handful of attempts
// with a doubling delay is enough to ride out a NATS instance that is briefly slow to come up (#365)
// without turning a longer outage into an indefinite hang.
const (
	configureStreamAttempts  = 5
	configureStreamBaseDelay = 1 * time.Second
)

// ConfigureStream ensures a stream exists with the given configuration, retrying while NATS is not
// answering yet.
//
// It stands where an os.Exit(1) on the first JetStream error used to: in Kubernetes, a NATS pod
// briefly slow to come up made the dispatcher crash-loop, which only amplified the load on NATS. The
// error is returned instead, so each caller decides whether that is fatal for it.
//
// One implementation for every stream, so that a second one cannot come to ride out a shorter
// outage than the first. The configuration stays with whoever owns the stream: this decides only how
// hard to try.
func ConfigureStream(ctx context.Context, sc StreamCreator, cfg jetstream.StreamConfig) error {
	return configureStreamWithRetry(ctx, sc, cfg, configureStreamAttempts, configureStreamBaseDelay)
}

// configureStreamWithRetry retries sc.CreateOrUpdateStream up to attempts times, waiting
// baseDelay*2^i between attempt i and i+1. It honours ctx cancellation while waiting instead of
// sleeping blindly, so a shutdown during startup returns promptly rather than burning the rest of
// the backoff.
func configureStreamWithRetry(ctx context.Context, sc StreamCreator, cfg jetstream.StreamConfig, attempts int, baseDelay time.Duration) error {
	var lastErr error
	for attempt := range attempts {
		_, err := sc.CreateOrUpdateStream(ctx, cfg)
		if err == nil {
			slog.Info(fmt.Sprintf("created js stream: %v", cfg.Name))
			return nil
		}
		lastErr = err

		if attempt == attempts-1 {
			break
		}

		delay := baseDelay * time.Duration(1<<attempt)
		slog.Warn("cannot create js stream, retrying",
			"stream", cfg.Name, "err", err, "attempt", attempt+1, "delay", delay)
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(delay):
		}
	}

	return fmt.Errorf("cannot create js stream %v after %d attempts: %w", cfg.Name, attempts, lastErr)
}
