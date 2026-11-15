// Package transient tells a failure that clears by itself — a Postgres or NATS that is restarting,
// a dropped connection, a timeout — from one that needs an operator, and retries the first kind
// quietly so that only a failure that outlasts its retries reaches the log as a warning or an error.
package transient

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"math/rand/v2"
	"net"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/nats-io/nats.go"
)

var natsTransient = []error{
	nats.ErrNoServers,
	nats.ErrConnectionClosed,
	nats.ErrConnectionDraining,
	nats.ErrConnectionReconnecting,
	nats.ErrDisconnected,
	nats.ErrStaleConnection,
	nats.ErrTimeout,
	nats.ErrNoResponders,
}

var syscallTransient = []error{
	syscall.ECONNREFUSED,
	syscall.ECONNRESET,
	syscall.ECONNABORTED,
	syscall.EPIPE,
	syscall.EHOSTUNREACH,
	syscall.ENETUNREACH,
}

// Is reports whether err is expected to clear by itself if the operation is tried again.
//
// A cancelled context is not transient: it is the caller giving up, and retrying would ignore it.
func Is(err error) bool {
	if err == nil || errors.Is(err, context.Canceled) {
		return false
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
		return true
	}
	for _, target := range natsTransient {
		if errors.Is(err, target) {
			return true
		}
	}
	for _, target := range syscallTransient {
		if errors.Is(err, target) {
			return true
		}
	}
	if pgconn.SafeToRetry(err) || pgconn.Timeout(err) {
		return true
	}
	var connectErr *pgconn.ConnectError
	if errors.As(err, &connectErr) {
		return true
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return transientSQLState(pgErr.Code)
	}
	var netErr net.Error
	return errors.As(err, &netErr)
}

// transientSQLState covers connection exceptions (class 08), a server shutting down or starting up,
// too many connections, and the serialization and deadlock failures Postgres asks clients to retry.
func transientSQLState(code string) bool {
	if len(code) == 5 && code[:2] == "08" {
		return true
	}
	switch code {
	case "57P01", "57P02", "57P03", "53300", "40001", "40P01":
		return true
	}
	return false
}

// Policy bounds a retry: how many attempts, how the delay between them grows, and for how long a
// failing attempt is only worth a debug line.
type Policy struct {
	Attempts int
	Base     time.Duration
	Max      time.Duration
	// Quiet is how long, from the first attempt, failures are logged at debug. A dependency that is
	// still unreachable past it is worth a warning even though the retry carries on.
	Quiet time.Duration
}

// Startup is the policy for reaching a dependency while a process boots: in Kubernetes the pods of
// Postgres, NATS and Kannon routinely come up in any order, so the first attempts are expected to
// fail. Roughly two minutes in all before the process gives up and lets its supervisor restart it.
var Startup = Policy{Attempts: 12, Base: 500 * time.Millisecond, Max: 15 * time.Second, Quiet: 20 * time.Second}

func (p Policy) delay(attempt int) time.Duration {
	d := p.Base << attempt
	if d <= 0 || d > p.Max {
		d = p.Max
	}
	jitter := time.Duration(rand.Int64N(int64(d)/5 + 1))
	return d - d/10 + jitter
}

// Do calls fn until it succeeds, fails with an error that is not transient, or p runs out of
// attempts, and returns the last error. log describes what is being attempted; it receives a debug
// line per failure inside p.Quiet, a warning per failure after it, and an info line when an attempt
// succeeds after earlier ones failed.
func Do[T any](ctx context.Context, log *slog.Logger, p Policy, fn func(context.Context) (T, error)) (T, error) {
	start := time.Now()
	var zero T
	var err error
	attempts := max(p.Attempts, 1)
	for attempt := range attempts {
		var v T
		v, err = fn(ctx)
		if err == nil {
			if attempt > 0 {
				log.Info("ready after retrying", "attempts", attempt+1, "elapsed", time.Since(start).Round(time.Millisecond))
			}
			return v, nil
		}
		if !Is(err) || attempt == attempts-1 {
			return zero, err
		}

		delay := p.delay(attempt)
		level := slog.LevelDebug
		if time.Since(start) >= p.Quiet {
			level = slog.LevelWarn
		}
		log.Log(ctx, level, "not ready yet, retrying", "attempt", attempt+1, "retry_in", delay.Round(time.Millisecond), "err", err)

		select {
		case <-ctx.Done():
			return zero, ctx.Err()
		case <-time.After(delay):
		}
	}
	return zero, err
}

// Level is the level for a failure that is handled by trying again later: a warning when err is
// expected to clear by itself, an error otherwise.
func Level(err error) slog.Level {
	if Is(err) {
		return slog.LevelWarn
	}
	return slog.LevelError
}
