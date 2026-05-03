package delivery

import (
	"math"
	"time"
)

// BackoffPolicy decides how long to wait before the next retry given the
// number of attempts already made.
type BackoffPolicy interface {
	Delay(attempts int) time.Duration
}

// ExponentialBackoff doubles the wait on each attempt, with a hard floor.
//
// delay = max(Min, Base * 2^attempts)
type ExponentialBackoff struct {
	Base time.Duration
	Min  time.Duration
}

func (e ExponentialBackoff) Delay(attempts int) time.Duration {
	delay := e.Base * time.Duration(math.Pow(2, float64(attempts)))
	if delay < e.Min {
		return e.Min
	}
	return delay
}

// DefaultBackoff reproduces the curve historically hardcoded in
// rescheduleDelay: 2m * 2^attempts, floored at 5m.
var DefaultBackoff BackoffPolicy = ExponentialBackoff{
	Base: 2 * time.Minute,
	Min:  5 * time.Minute,
}
