package stats

import (
	"context"
	"sync"
	"time"
)

// InMemAggregatedStatsRepository is an in-memory implementation of AggregatedStatsRepository for testing.
type InMemAggregatedStatsRepository struct {
	mu    sync.Mutex
	stats map[aggregatedKey]*AggregatedStat
}

type aggregatedKey struct {
	Domain    string
	Timestamp time.Time
	Type      Type
}

// NewInMemAggregatedStatsRepository creates a new in-memory aggregated stats repository.
func NewInMemAggregatedStatsRepository() *InMemAggregatedStatsRepository {
	return &InMemAggregatedStatsRepository{
		stats: make(map[aggregatedKey]*AggregatedStat),
	}
}

func (r *InMemAggregatedStatsRepository) Increment(_ context.Context, domain string, timestamp time.Time, statType Type) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	k := aggregatedKey{Domain: domain, Timestamp: timestamp, Type: statType}
	if existing, ok := r.stats[k]; ok {
		existing.Count++
	} else {
		r.stats[k] = &AggregatedStat{
			Type:      statType,
			Timestamp: timestamp,
			Count:     1,
		}
	}
	return nil
}

func (r *InMemAggregatedStatsRepository) Query(_ context.Context, domain string, timeRange TimeRange) ([]*AggregatedStat, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	var result []*AggregatedStat
	for k, v := range r.stats {
		if k.Domain != domain {
			continue
		}
		if k.Timestamp.Before(timeRange.Start) || k.Timestamp.After(timeRange.Stop) {
			continue
		}
		cp := *v
		result = append(result, &cp)
	}
	return result, nil
}
