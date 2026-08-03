package dispatcher

// retryConfigureSendingStream replaced an os.Exit(1) on the first JetStream
// error (#365): in Kubernetes a NATS instance that is briefly slow to come up
// made the dispatcher crash-loop, amplifying load on NATS. These tests
// exercise the retry/backoff loop directly against a fake streamCreator, so
// they run in milliseconds instead of the real multi-second backoff and need
// no NATS connection at all.

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeStreamCreator is a minimal streamCreator whose CreateOrUpdateStream
// behaviour is driven by a test-supplied function, so tests can force a
// given number of failures before success (or permanent failure) without a
// real JetStream connection.
type fakeStreamCreator struct {
	calls int32
	do    func(calls int32) error
}

func (f *fakeStreamCreator) CreateOrUpdateStream(_ context.Context, _ jetstream.StreamConfig) (jetstream.Stream, error) {
	calls := atomic.AddInt32(&f.calls, 1)
	if err := f.do(calls); err != nil {
		return nil, err
	}
	return nil, nil
}

func TestRetryConfigureSendingStreamSucceedsOnLaterAttempt(t *testing.T) {
	ctx := t.Context()

	failUntil := int32(3) // first 2 calls fail, the 3rd succeeds
	sc := &fakeStreamCreator{
		do: func(calls int32) error {
			if calls < failUntil {
				return errors.New("nats: no responders available for request")
			}
			return nil
		},
	}

	err := retryConfigureSendingStream(ctx, sc, 5, time.Millisecond)

	require.NoError(t, err)
	assert.Equal(t, failUntil, atomic.LoadInt32(&sc.calls),
		"should stop retrying as soon as CreateOrUpdateStream succeeds")
}

func TestRetryConfigureSendingStreamFailsAfterExhaustingAttempts(t *testing.T) {
	ctx := t.Context()

	wantErr := errors.New("nats: no responders available for request")
	sc := &fakeStreamCreator{
		do: func(int32) error {
			return wantErr
		},
	}

	const attempts = 3
	err := retryConfigureSendingStream(ctx, sc, attempts, time.Millisecond)

	require.Error(t, err)
	assert.ErrorIs(t, err, wantErr)
	assert.Equal(t, int32(attempts), atomic.LoadInt32(&sc.calls),
		"should attempt exactly `attempts` times, no more and no less")
}

func TestRetryConfigureSendingStreamHonoursContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())

	sc := &fakeStreamCreator{
		do: func(calls int32) error {
			if calls == 1 {
				// Cancel only after the first failed attempt, while the
				// helper is about to wait between attempts.
				cancel()
			}
			return errors.New("nats: no responders available for request")
		},
	}

	// A large attempt count and base delay: if cancellation were not
	// honoured, this call would block for a long time. The assertion below
	// on elapsed time is what actually proves the backoff was skipped.
	const attempts = 10
	baseDelay := 10 * time.Second

	start := time.Now()
	err := retryConfigureSendingStream(ctx, sc, attempts, baseDelay)
	elapsed := time.Since(start)

	require.Error(t, err)
	assert.ErrorIs(t, err, context.Canceled)
	assert.Less(t, elapsed, time.Second,
		"context cancellation should return promptly instead of waiting out the backoff")
	assert.Equal(t, int32(1), atomic.LoadInt32(&sc.calls),
		"should not attempt again once the context is cancelled while waiting")
}
