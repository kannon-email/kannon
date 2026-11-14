package mailflow

import (
	"context"
	"log/slog"
	"time"

	"github.com/kannon-email/kannon/x/metrics"
	"github.com/prometheus/client_golang/prometheus"
)

const poolQueryTimeout = 2 * time.Second

// PoolCounter counts the Deliveries in the Pool by status.
type PoolCounter func(context.Context) (map[string]float64, error)

// RegisterPool registers the kannon_pool_deliveries gauge on r, read with count at scrape time. A
// process registering it twice keeps the first.
func RegisterPool(r prometheus.Registerer, count PoolCounter) {
	metrics.Register(r, &poolCollector{
		desc: prometheus.NewDesc("kannon_pool_deliveries",
			"Deliveries in the Pool, by status. The statuses are the Pool's internal mechanics, exposed for operators, and may change between releases.",
			[]string{"status"}, nil),
		count: count,
	})
}

type poolCollector struct {
	desc  *prometheus.Desc
	count PoolCounter
}

func (c *poolCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- c.desc
}

// Collect reports nothing when the query fails, so a slow or unreachable database costs this one
// metric and never the scrape.
func (c *poolCollector) Collect(ch chan<- prometheus.Metric) {
	ctx, cancel := context.WithTimeout(context.Background(), poolQueryTimeout)
	defer cancel()
	counts, err := c.count(ctx)
	if err != nil {
		slog.Warn("cannot count the Pool for kannon_pool_deliveries", "err", err)
		return
	}
	for status, n := range counts {
		ch <- prometheus.MustNewConstMetric(c.desc, prometheus.GaugeValue, n, status)
	}
}
