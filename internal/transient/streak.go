package transient

import (
	"context"
	"log/slog"
	"sync"
	"time"
)

const (
	// quietFailures is how many transient failures in a row stay at debug: a cycle that fails once
	// or twice while Postgres or NATS restarts and then recovers is not news.
	quietFailures = 3
	// reportEvery spaces out the reports of a failure that keeps happening, so that a cycle running
	// every second does not write a warning every second for the length of an outage.
	reportEvery = time.Minute
)

// Streak logs the outcome of something attempted over and over — a periodic cycle, the handling of
// a redelivered message — so that a transient failure is reported only once it repeats, a lasting
// one is reported once a minute rather than once per attempt, and the recovery from a reported
// failure is reported too. Safe for concurrent use.
type Streak struct {
	log *slog.Logger

	mu         sync.Mutex
	failures   int
	firstAt    time.Time
	reportedAt time.Time
}

// NewStreak returns a Streak logging to log, which should carry what is being attempted.
func NewStreak(log *slog.Logger) *Streak {
	return &Streak{log: log}
}

// Failed records a failed attempt and logs it at the level it deserves: debug while it is a short
// run of transient failures, a warning once a transient failure persists, an error at once for any
// other failure, and debug again between two reports of the same streak.
func (s *Streak) Failed(ctx context.Context, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	s.failures++
	if s.failures == 1 {
		s.firstAt = now
	}

	level := slog.LevelError
	if Is(err) {
		level = slog.LevelWarn
		if s.failures < quietFailures {
			level = slog.LevelDebug
		}
	}
	if level > slog.LevelDebug && !s.reportedAt.IsZero() && now.Sub(s.reportedAt) < reportEvery {
		level = slog.LevelDebug
	}
	if level > slog.LevelDebug {
		s.reportedAt = now
	}

	s.log.Log(ctx, level, "failed, will retry",
		"consecutive_failures", s.failures, "failing_for", now.Sub(s.firstAt).Round(time.Second), "err", err)
}

// Succeeded ends the current streak, logging the recovery if the streak had been reported.
func (s *Streak) Succeeded() {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.reportedAt.IsZero() {
		s.log.Info("recovered", "after_failures", s.failures, "failed_for", time.Since(s.firstAt).Round(time.Second))
	}
	s.failures = 0
	s.firstAt = time.Time{}
	s.reportedAt = time.Time{}
}
