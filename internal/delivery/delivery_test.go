package delivery

import (
	"testing"
	"time"

	"github.com/kannon-email/kannon/internal/batch"
	"github.com/kannon-email/kannon/internal/tracking"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNew(t *testing.T) {
	t.Run("Success", func(t *testing.T) {
		now := time.Now().UTC().Truncate(time.Second)
		d, err := New(NewParams{
			BatchID:       batch.NewID("example.com"),
			Email:         "to@example.com",
			Fields:        map[string]string{"name": "X"},
			Domain:        "example.com",
			ScheduledTime: now,
			Backoff:       DefaultBackoff,
		})
		require.NoError(t, err)
		assert.Equal(t, "to@example.com", d.Email())
		assert.Equal(t, "example.com", d.Domain())
		assert.Equal(t, 0, d.SendAttempts())
		assert.Equal(t, now, d.ScheduledTime())
		assert.Equal(t, now, d.OriginalScheduledTime())
	})

	t.Run("TrackingPolicyIsConcrete", func(t *testing.T) {
		// A Delivery is created with the Policy already resolved at intake; a
		// caller that states nothing must still leave a concrete Policy behind,
		// because the Pool row must never hold an unstated Mode.
		d, err := New(NewParams{
			BatchID: batch.NewID("example.com"),
			Email:   "to@example.com",
			Domain:  "example.com",
		})
		require.NoError(t, err)
		assert.Equal(t, tracking.Policy{Opens: tracking.ModeOff, Links: tracking.ModeOff}, d.TrackingPolicy())

		d, err = New(NewParams{
			BatchID:  batch.NewID("example.com"),
			Email:    "to@example.com",
			Domain:   "example.com",
			Tracking: tracking.Policy{Opens: tracking.ModeIdentified},
		})
		require.NoError(t, err)
		assert.Equal(t, tracking.Policy{Opens: tracking.ModeIdentified, Links: tracking.ModeOff}, d.TrackingPolicy())
	})

	t.Run("MissingFields", func(t *testing.T) {
		cases := []struct {
			name string
			p    NewParams
		}{
			{"no batch", NewParams{Email: "a@b.c", Domain: "b.c"}},
			{"no email", NewParams{BatchID: batch.NewID("b.c"), Domain: "b.c"}},
			{"no domain", NewParams{BatchID: batch.NewID("b.c"), Email: "a@b.c"}},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				_, err := New(tc.p)
				assert.Error(t, err)
			})
		}
	})
}

func TestCanRetry(t *testing.T) {
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	load := func(attempts int, window time.Duration) *Delivery {
		return Load(LoadParams{
			BatchID:               batch.NewID("example.com"),
			Email:                 "to@example.com",
			Domain:                "example.com",
			SendAttempts:          attempts,
			ScheduledTime:         base,
			OriginalScheduledTime: base,
			Backoff:               DefaultBackoff,
			RetryWindow:           window,
		})
	}

	// The equivalence with the retry cap this replaced. maxRetry = 10 inside
	// internal/envelope admitted attempts 0..9 and refused the 10th; under
	// DefaultBackoff the tenth retry falls at 2m·2⁹ = 17h04m and the eleventh at
	// 2m·2¹⁰ = 34h08m, so a 24h window admits exactly the same retries.
	t.Run("EquivalentToTheRetryCapItReplaced", func(t *testing.T) {
		assert.Equal(t, 17*time.Hour+4*time.Minute, DefaultBackoff.Delay(9))
		assert.Equal(t, 34*time.Hour+8*time.Minute, DefaultBackoff.Delay(10))

		assert.True(t, load(9, DefaultRetryWindow).CanRetry(),
			"the tenth retry falls at 17h04m, inside the 24h window")
		assert.False(t, load(10, DefaultRetryWindow).CanRetry(),
			"the eleventh retry falls at 34h08m, outside the 24h window")
	})

	t.Run("WindowDefaultsWhenZero", func(t *testing.T) {
		// A caller that states no window gets DefaultRetryWindow, so a missing
		// wire-up degrades to the production budget rather than to a Delivery
		// nothing will ever retry.
		assert.True(t, load(9, 0).CanRetry())
		assert.False(t, load(10, 0).CanRetry())

		// And a stated window is honoured: the same Delivery that has run out
		// under 24h still has room under 48h.
		assert.True(t, load(10, 48*time.Hour).CanRetry())
	})

	t.Run("FirstAttemptSurvivesArbitraryLateness", func(t *testing.T) {
		// The design's own claim: both instants CanRetry compares derive from
		// originalScheduledTime, so lateness cannot spend the budget. A
		// Dispatcher that was down for a week does not mass-terminate the Batch
		// it finds waiting on resumption — the guarantee holds by construction
		// rather than by a special case for the first attempt.
		late := Load(LoadParams{
			BatchID:               batch.NewID("example.com"),
			Email:                 "to@example.com",
			Domain:                "example.com",
			ScheduledTime:         time.Now().UTC().Add(-7 * 24 * time.Hour),
			OriginalScheduledTime: time.Now().UTC().Add(-7 * 24 * time.Hour),
			Backoff:               DefaultBackoff,
			RetryWindow:           DefaultRetryWindow,
		})
		assert.True(t, late.CanRetry(), "a Delivery offered its first attempt a week late still gets one")
	})

	t.Run("NewCarriesTheWindow", func(t *testing.T) {
		d, err := New(NewParams{
			BatchID:       batch.NewID("example.com"),
			Email:         "to@example.com",
			Domain:        "example.com",
			ScheduledTime: base,
			Backoff:       DefaultBackoff,
			RetryWindow:   time.Nanosecond,
		})
		require.NoError(t, err)
		// A Delivery created at intake carries the same budget as one rehydrated
		// from its row, so the two cannot disagree about when it is over.
		assert.False(t, d.CanRetry())
	})
}

func TestNextRetryAt(t *testing.T) {
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	cases := []struct {
		attempts int
		want     time.Duration
	}{
		{0, 5 * time.Minute},  // 2*1min < 5min => floor at 5min
		{1, 5 * time.Minute},  // 2*2min < 5min => floor at 5min
		{2, 8 * time.Minute},  // 2*4 = 8min
		{3, 16 * time.Minute}, // 2*8 = 16min
		{4, 32 * time.Minute},
	}

	for _, tc := range cases {
		d := Load(LoadParams{
			BatchID:               batch.NewID("example.com"),
			Email:                 "to@example.com",
			Domain:                "example.com",
			SendAttempts:          tc.attempts,
			ScheduledTime:         base,
			OriginalScheduledTime: base,
			Backoff:               DefaultBackoff,
		})
		assert.Equal(t, base.Add(tc.want), d.NextRetryAt(), "attempts=%d", tc.attempts)
	}
}
