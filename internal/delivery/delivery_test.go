package delivery

import (
	"testing"
	"time"

	"github.com/kannon-email/kannon/internal/batch"
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
