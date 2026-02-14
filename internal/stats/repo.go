package stats

import (
	"context"
	"time"
)

// TimeRange represents a time interval for queries.
type TimeRange struct {
	Start time.Time
	Stop  time.Time
}

// Pagination represents offset-based pagination parameters.
type Pagination struct {
	Limit  int
	Offset int
}

// Repository defines the interface for stats persistence operations.
type Repository interface {
	// Insert persists a new stat event.
	Insert(ctx context.Context, stat *Stat) error

	// Query returns stats for a domain within a time range, with pagination.
	Query(ctx context.Context, domain string, timeRange TimeRange, page Pagination) ([]*Stat, error)

	// Count returns the total number of stats for a domain within a time range.
	Count(ctx context.Context, domain string, timeRange TimeRange) (int64, error)

	// QueryTimeline returns aggregated (hourly) stats for a domain within a time range.
	QueryTimeline(ctx context.Context, domain string, timeRange TimeRange) ([]*AggregatedStat, error)

	// DeleteOlderThan removes stats older than the given time, returning the count of deleted rows.
	DeleteOlderThan(ctx context.Context, before time.Time) (int64, error)
}
