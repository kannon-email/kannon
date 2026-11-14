package metrics

import (
	"errors"

	"github.com/prometheus/client_golang/prometheus"
)

// Register registers c on r and returns it, or returns the equal collector r already holds, so a
// Component constructed twice on one container counts into one series rather than panicking.
func Register[T prometheus.Collector](r prometheus.Registerer, c T) T {
	if err := r.Register(c); err != nil {
		var already prometheus.AlreadyRegisteredError
		if errors.As(err, &already) {
			if existing, ok := already.ExistingCollector.(T); ok {
				return existing
			}
		}
		panic(err)
	}
	return c
}
