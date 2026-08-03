package stats

import (
	"context"
	"time"

	"github.com/kannon-email/kannon/internal/values"
)

// AggregatedStatsRepository defines persistence operations for aggregated stats. As with Repository,
// the Domain is named by its canonical domain name: a counter incremented under one spelling and
// read under another would silently be two counters.
type AggregatedStatsRepository interface {
	Increment(ctx context.Context, domain values.DomainName, timestamp time.Time, statType Type) error
	Query(ctx context.Context, domain values.DomainName, timeRange TimeRange) ([]*AggregatedStat, error)
}
