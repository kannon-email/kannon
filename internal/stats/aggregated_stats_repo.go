package stats

import (
	"context"
	"time"
)

// AggregatedStatsRepository defines persistence operations for aggregated stats.
type AggregatedStatsRepository interface {
	Increment(ctx context.Context, domain string, timestamp time.Time, statType Type) error
	Query(ctx context.Context, domain string, timeRange TimeRange) ([]*AggregatedStat, error)
}
