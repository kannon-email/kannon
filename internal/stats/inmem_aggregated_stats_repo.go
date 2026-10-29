package stats

import (
	"context"
	"sync"
	"time"

	"github.com/kannon-email/kannon/internal/values"
)

// InMemAggregatedStatsRepository is an in-memory implementation of AggregatedStatsRepository for testing.
type InMemAggregatedStatsRepository struct {
	mu      sync.Mutex
	stats   map[aggregatedKey]*AggregatedStat
	counted map[StoredEventID]struct{}
}

type aggregatedKey struct {
	Domain    values.DomainName
	Timestamp time.Time
	Type      Type
}

func NewInMemAggregatedStatsRepository() *InMemAggregatedStatsRepository {
	return &InMemAggregatedStatsRepository{
		stats:   make(map[aggregatedKey]*AggregatedStat),
		counted: make(map[StoredEventID]struct{}),
	}
}

func (r *InMemAggregatedStatsRepository) Increment(_ context.Context, domain values.DomainName, timestamp time.Time, statType Type) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.incrementLocked(domain, timestamp, statType)
	return nil
}

// incrementLocked updates a bucket while the caller holds mu.
func (r *InMemAggregatedStatsRepository) incrementLocked(domain values.DomainName, timestamp time.Time, statType Type) {
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
}

func (r *InMemAggregatedStatsRepository) Query(_ context.Context, domain values.DomainName, timeRange TimeRange) ([]*AggregatedStat, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	var result []*AggregatedStat
	for k, v := range r.stats {
		if k.Domain != domain {
			continue
		}
		if k.Timestamp.Before(timeRange.Start) || !k.Timestamp.Before(timeRange.Stop) {
			continue
		}
		cp := *v
		result = append(result, &cp)
	}
	return result, nil
}

// Count counts a stored event at most once, under the same lock as its marker.
func (r *InMemAggregatedStatsRepository) Count(ctx context.Context, id StoredEventID, domain values.DomainName, timestamp time.Time, statType Type) error {
	if err := id.Validate(); err != nil {
		return err
	}
	id.StreamCreated = id.StreamCreated.UTC().Round(0)
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	if _, ok := r.counted[id]; ok {
		return nil
	}
	r.incrementLocked(domain, timestamp.UTC().Truncate(time.Hour), statType)
	r.counted[id] = struct{}{}
	return nil
}
