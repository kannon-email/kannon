package cmd

import (
	"maps"

	"github.com/kannon-email/kannon/pkg/api"
	"github.com/kannon-email/kannon/pkg/tracker"
	"github.com/kannon-email/kannon/x/config"
	"github.com/kannon-email/kannon/x/metrics"
)

// loadMetrics reads the `metrics` section and refuses a listener that would collide with one of the
// Components this process runs. A Component it does not run opens no port, so cannot collide.
func loadMetrics(services config.Services) (metrics.Config, error) {
	cfg, err := metrics.LoadConfig()
	if err != nil {
		return metrics.Config{}, err
	}
	ports := map[string]uint{}
	for _, listener := range []struct {
		enabled bool
		ports   func() (map[string]uint, error)
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
		maps.Copy(ports, p)
	}
	return cfg, cfg.RequireOwnPort(ports)
}
