package container

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
)

func newMetricsRegistry() *prometheus.Registry {
	reg := prometheus.NewRegistry()
	reg.MustRegister(
		collectors.NewGoCollector(),
		collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
	)
	return reg
}

// Metrics returns the process's one Prometheus registry. Every Component registers its own
// collectors on it, and the metrics listener serves it.
func (c *Container) Metrics() *prometheus.Registry {
	return c.metrics
}
