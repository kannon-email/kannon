package envelope

import (
	"errors"
	"fmt"
	"testing"

	"github.com/kannon-email/kannon/internal/tracking"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func anonymousKey(messageID string) sharedTokenKey {
	return sharedTokenKey{
		axis:      tracking.AxisOpens,
		domain:    "test.com",
		messageID: messageID,
		mode:      tracking.ModeAnonymous,
	}
}

// TestSharedTokensAreBounded is the leak test. The Dispatcher builds across
// unboundedly many Batches over a process lifetime, so the thing that must hold is
// not "the cache is useful" but "the cache stops growing".
func TestSharedTokensAreBounded(t *testing.T) {
	c := newSharedTokens()

	for i := range 10 * sharedTokenGeneration {
		key := anonymousKey(fmt.Sprintf("msg-%d@test.com", i))
		token, err := c.reuse(key, func() (string, error) { return fmt.Sprintf("tok-%d", i), nil })
		require.NoError(t, err)
		require.Equal(t, fmt.Sprintf("tok-%d", i), token)
		require.LessOrEqual(t, c.len(), 2*sharedTokenGeneration,
			"the cache must never hold more than two generations")
	}
}

// TestSharedTokensSurviveARotation covers the reason there are two generations
// rather than one: a Batch still being dispatched when the live generation rotates
// must keep its token, or the property that every Recipient of a Batch holds the
// same token would quietly stop holding for large Batches.
func TestSharedTokensSurviveARotation(t *testing.T) {
	c := newSharedTokens()

	inFlight := anonymousKey("msg-in-flight@test.com")
	first, err := c.reuse(inFlight, func() (string, error) { return "original", nil })
	require.NoError(t, err)

	// Fill a whole generation with other Batches, which rotates the one the
	// in-flight Batch landed in.
	for i := range sharedTokenGeneration {
		_, err := c.reuse(anonymousKey(fmt.Sprintf("msg-%d@test.com", i)),
			func() (string, error) { return "other", nil })
		require.NoError(t, err)
	}

	again, err := c.reuse(inFlight, func() (string, error) {
		t.Error("the in-flight Batch must not be asked to sign again")
		return "", errors.New("unexpected mint")
	})
	require.NoError(t, err)
	assert.Equal(t, first, again)
}

// TestSharedTokensDoNotCacheAFailedMint keeps a transient signing failure — an
// unreachable database, say — from being remembered as the answer.
func TestSharedTokensDoNotCacheAFailedMint(t *testing.T) {
	c := newSharedTokens()
	key := anonymousKey("msg-1@test.com")

	_, err := c.reuse(key, func() (string, error) { return "", errors.New("no signing key") })
	require.Error(t, err)
	assert.Zero(t, c.len(), "a failed mint must leave nothing behind")

	token, err := c.reuse(key, func() (string, error) { return "tok", nil })
	require.NoError(t, err)
	assert.Equal(t, "tok", token)
}
