package audit

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"testing"

	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The warning exists for one deployment mistake: collection enabled and the worker forgotten, whose
// Audit Records sit on the stream and expire unread. It fires on the conjunction — Records pending
// *and* no consumer — because either half alone is an ordinary moment in a healthy deployment, and a
// warning that appeared on every boot would be trained out of an operator within a week.
func TestTheBacklogWarningFiresOnlyWhenRecordsAreActuallyPilingUp(t *testing.T) {
	tests := []struct {
		name      string
		pending   uint64
		consumers int
		wantWarn  bool
	}{
		{
			name:      "records pending with nobody consuming them",
			pending:   42,
			consumers: 0,
			wantWarn:  true,
		},
		{
			name:      "records pending with a consumer working through them",
			pending:   42,
			consumers: 1,
			wantWarn:  false,
		},
		{
			name:      "no consumer yet, and nothing lost by that",
			pending:   0,
			consumers: 0,
			wantWarn:  false,
		},
		{
			name:      "a healthy deployment between messages",
			pending:   0,
			consumers: 1,
			wantWarn:  false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			logged := captureSlog(t)

			warnOnBacklog(t.Context(), &fakeStreamReader{
				stream: &fakeStream{info: &jetstream.StreamInfo{
					State: jetstream.StreamState{Msgs: tc.pending, Consumers: tc.consumers},
				}},
			})

			if tc.wantWarn {
				assert.Contains(t, logged.String(), "level=WARN")
				assert.Contains(t, logged.String(), "services.audit.enabled",
					"the warning has to name what the operator is missing")
			} else {
				assert.NotContains(t, logged.String(), "level=WARN")
			}
		})
	}
}

// A stream this process cannot read is not itself worth an operator's attention, and it must not be:
// the check runs beside an API that has to stay up, and shouting about NATS here would be a second,
// worse monitor of something the container's health checks already report on.
func TestTheBacklogCheckStaysQuietWhenItCannotReadTheStream(t *testing.T) {
	unreachable := errors.New("nats: no responders available for request")

	for _, reader := range []streamReader{
		&fakeStreamReader{err: unreachable},
		&fakeStreamReader{stream: &fakeStream{err: unreachable}},
	} {
		logged := captureSlog(t)
		warnOnBacklog(t.Context(), reader)
		assert.NotContains(t, logged.String(), "level=WARN")
	}
}

// fakeStreamReader stands in for a JetStream connection. jetstream.JetStream is a wide interface and
// this check needs one method of it, which is why streamReader is that one method.
type fakeStreamReader struct {
	stream jetstream.Stream
	err    error
}

func (f *fakeStreamReader) Stream(context.Context, string) (jetstream.Stream, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.stream, nil
}

// fakeStream reports whatever state a test gives it. jetstream.Stream is embedded so that any method
// this check starts calling without a test noticing panics rather than returning a zero value.
type fakeStream struct {
	jetstream.Stream
	info *jetstream.StreamInfo
	err  error
}

func (f *fakeStream) Info(context.Context, ...jetstream.StreamInfoOpt) (*jetstream.StreamInfo, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.info, nil
}

// WatchBacklog does not check at once, so that a worker coming up alongside this process is not warned
// about. Asserted through a cancelled context: the wait is the first thing it does, so it returns the
// cancellation without ever having looked at the stream.
func TestWatchBacklogDoesNotCheckBeforeItsFirstInterval(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	reader := &fakeStreamReader{err: errors.New("Stream must not be called")}
	err := WatchBacklog(ctx, reader)

	require.ErrorIs(t, err, context.Canceled)
}

// captureSlog redirects the default logger for the duration of one test, at debug so that the level a
// line was written at is observable rather than filtered out by the handler.
func captureSlog(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return &buf
}
