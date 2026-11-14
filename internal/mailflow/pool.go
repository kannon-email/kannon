package mailflow

import (
	"context"
	"log/slog"
	"time"

	"github.com/kannon-email/kannon/internal/pool"
	"github.com/kannon-email/kannon/x/metrics"
	"github.com/prometheus/client_golang/prometheus"
)

const poolQueryTimeout = 2 * time.Second

// RegisterPool registers the kannon_pool_deliveries gauge on r, read from census at scrape time.
// Every pool.Status gets a series on every scrape, so none vanishes when the Pool empties. A process
// registering it twice keeps the first.
func RegisterPool(r prometheus.Registerer, census pool.Census) {
	metrics.Register(r, &poolCollector{
		desc: prometheus.NewDesc("kannon_pool_deliveries",
			"Deliveries in the Pool, by status. The statuses are the Pool's internal mechanics, exposed for operators, and may change between releases.",
			[]string{"status"}, nil),
		census: census,
	})
}

type poolCollector struct {
	desc   *prometheus.Desc
	census pool.Census
}

func (c *poolCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- c.desc
}

// Collect reports nothing when the query fails, so a slow or unreachable database costs this one
// metric and never the scrape.
func (c *poolCollector) Collect(ch chan<- prometheus.Metric) {
	ctx, cancel := context.WithTimeout(context.Background(), poolQueryTimeout)
	defer cancel()
	counts, err := c.census.DeliveriesByStatus(ctx)
	if err != nil {
		slog.Warn("cannot count the Pool for kannon_pool_deliveries", "err", err)
		return
	}
	for _, status := range pool.Statuses() {
		ch <- prometheus.MustNewConstMetric(c.desc, prometheus.GaugeValue, float64(counts[status]), string(status))
	}
}
