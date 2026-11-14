package cmd

import (
	"github.com/kannon-email/kannon/pkg/api"
	"github.com/kannon-email/kannon/pkg/tracker"
	"github.com/kannon-email/kannon/x/config"
	"github.com/kannon-email/kannon/x/metrics"
)

// loadMetrics reads the `metrics` section and refuses a listener that would collide with one the
// process actually runs. A Component it does not run opens no port, so cannot collide.
func loadMetrics(services config.Services) (metrics.Config, error) {
	cfg, err := metrics.LoadConfig()
	if err != nil {
		return metrics.Config{}, err
	}
	var ports []metrics.ListenPort
	for _, listener := range []struct {
		enabled bool
		ports   func() ([]metrics.ListenPort, error)
	}{
		{services.API.Enabled, api.ListenPorts},
		{services.Tracker.Enabled, tracker.ListenPorts},
	} {
		if !listener.enabled {
			continue
		}
		p, err := listener.ports()
		if err != nil {
			return metrics.Config{}, err
		}
		ports = append(ports, p...)
	}
	return cfg, cfg.RequireOwnPort(ports)
}
