package delivery

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestDefaultBackoffCurve(t *testing.T) {
	cases := []struct {
		attempts int
		want     time.Duration
	}{
		{0, 5 * time.Minute},  // 2m * 1 = 2m, floored to 5m
		{1, 5 * time.Minute},  // 2m * 2 = 4m, floored to 5m
		{2, 8 * time.Minute},  // 2m * 4 = 8m, exponential dominates
		{3, 16 * time.Minute}, // 2m * 8
		{4, 32 * time.Minute}, // 2m * 16
		{5, 64 * time.Minute}, // 2m * 32
	}
	for _, tc := range cases {
		assert.Equal(t, tc.want, DefaultBackoff.Delay(tc.attempts), "attempts=%d", tc.attempts)
	}
}

func TestExponentialBackoffFloorBinds(t *testing.T) {
	p := ExponentialBackoff{Base: 1 * time.Second, Min: 10 * time.Second}
	assert.Equal(t, 10*time.Second, p.Delay(0))
	assert.Equal(t, 10*time.Second, p.Delay(1))
	assert.Equal(t, 10*time.Second, p.Delay(2))
	assert.Equal(t, 16*time.Second, p.Delay(4))
}
