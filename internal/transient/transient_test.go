package transient

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/nats-io/nats.go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIs(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"plain", errors.New("boom"), false},
		{"cancelled", context.Canceled, false},
		{"deadline", context.DeadlineExceeded, true},
		{"eof", io.EOF, true},
		{"nats no servers", nats.ErrNoServers, true},
		{"nats wrapped timeout", fmt.Errorf("publish: %w", nats.ErrTimeout), true},
		{"nats authorization", nats.ErrAuthorization, false},
		{"connection refused", fmt.Errorf("dial: %w", syscall.ECONNREFUSED), true},
		{"connection reset", syscall.ECONNRESET, true},
		{"pg admin shutdown", &pgconn.PgError{Code: "57P01"}, true},
		{"pg connection failure", &pgconn.PgError{Code: "08006"}, true},
		{"pg serialization", &pgconn.PgError{Code: "40001"}, true},
		{"pg unique violation", &pgconn.PgError{Code: "23505"}, false},
		{"pg undefined table", &pgconn.PgError{Code: "42P01"}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, Is(tc.err))
		})
	}
}

var fastPolicy = Policy{Attempts: 4, Base: time.Millisecond, Max: 2 * time.Millisecond, Quiet: time.Hour}

func captureLog(level slog.Level) (*slog.Logger, *bytes.Buffer) {
	var buf bytes.Buffer
	return slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: level})), &buf
}

func TestDoRetriesTransientFailuresQuietly(t *testing.T) {
	log, buf := captureLog(slog.LevelInfo)
	calls := 0
	v, err := Do(t.Context(), log, fastPolicy, func(context.Context) (int, error) {
		calls++
		if calls < 3 {
			return 0, nats.ErrNoServers
		}
		return 42, nil
	})

	require.NoError(t, err)
	assert.Equal(t, 42, v)
	assert.Equal(t, 3, calls)
	assert.NotContains(t, buf.String(), "level=WARN")
	assert.Contains(t, buf.String(), "ready after retrying")
}

func TestDoStopsOnAPermanentFailure(t *testing.T) {
	log, _ := captureLog(slog.LevelInfo)
	calls := 0
	_, err := Do(t.Context(), log, fastPolicy, func(context.Context) (int, error) {
		calls++
		return 0, nats.ErrAuthorization
	})

	assert.ErrorIs(t, err, nats.ErrAuthorization)
	assert.Equal(t, 1, calls)
}

func TestDoGivesUpAfterItsAttempts(t *testing.T) {
	log, _ := captureLog(slog.LevelInfo)
	calls := 0
	_, err := Do(t.Context(), log, fastPolicy, func(context.Context) (int, error) {
		calls++
		return 0, nats.ErrNoServers
	})

	assert.ErrorIs(t, err, nats.ErrNoServers)
	assert.Equal(t, fastPolicy.Attempts, calls)
}

func TestDoWarnsOncePastTheQuietWindow(t *testing.T) {
	log, buf := captureLog(slog.LevelInfo)
	p := fastPolicy
	p.Quiet = 0
	_, err := Do(t.Context(), log, p, func(context.Context) (int, error) {
		return 0, nats.ErrNoServers
	})

	assert.Error(t, err)
	assert.Contains(t, buf.String(), "level=WARN")
}

func TestDoHonoursCancellation(t *testing.T) {
	log, _ := captureLog(slog.LevelInfo)
	ctx, cancel := context.WithCancel(t.Context())
	p := Policy{Attempts: 5, Base: time.Hour, Max: time.Hour}
	_, err := Do(ctx, log, p, func(context.Context) (int, error) {
		cancel()
		return 0, nats.ErrNoServers
	})

	assert.ErrorIs(t, err, context.Canceled)
}

func levels(buf *bytes.Buffer) []string {
	var out []string
	for line := range strings.Lines(buf.String()) {
		for _, f := range strings.Fields(line) {
			if lvl, ok := strings.CutPrefix(f, "level="); ok {
				out = append(out, lvl)
			}
		}
	}
	return out
}

func TestStreakKeepsShortTransientRunsAtDebug(t *testing.T) {
	log, buf := captureLog(slog.LevelDebug)
	s := NewStreak(log)

	s.Failed(t.Context(), nats.ErrNoServers)
	s.Failed(t.Context(), nats.ErrNoServers)
	s.Succeeded()

	assert.Equal(t, []string{"DEBUG", "DEBUG"}, levels(buf))
}

func TestStreakWarnsOnAPersistingTransientFailureAndReportsRecovery(t *testing.T) {
	log, buf := captureLog(slog.LevelDebug)
	s := NewStreak(log)

	for range 5 {
		s.Failed(t.Context(), nats.ErrNoServers)
	}
	s.Succeeded()

	assert.Equal(t, []string{"DEBUG", "DEBUG", "WARN", "DEBUG", "DEBUG", "INFO"}, levels(buf))
	assert.Contains(t, buf.String(), "recovered")
}

func TestStreakReportsAPermanentFailureAtOnce(t *testing.T) {
	log, buf := captureLog(slog.LevelDebug)
	s := NewStreak(log)

	s.Failed(t.Context(), errors.New("column does not exist"))
	s.Failed(t.Context(), errors.New("column does not exist"))

	assert.Equal(t, []string{"ERROR", "DEBUG"}, levels(buf))
}
