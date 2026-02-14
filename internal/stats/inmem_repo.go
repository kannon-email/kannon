package stats

import (
	"context"
	"sort"
	"sync"
	"time"
)

// InMemRepository is an in-memory implementation of Repository for testing.
type InMemRepository struct {
	mu     sync.Mutex
	stats  []*Stat
	nextID int32
}

// NewInMemRepository creates a new in-memory stats repository.
func NewInMemRepository() *InMemRepository {
	return &InMemRepository{nextID: 1}
}

func (r *InMemRepository) Insert(_ context.Context, stat *Stat) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	stat.ID = r.nextID
	r.nextID++

	// Store a copy to avoid external mutation.
	cp := *stat
	r.stats = append(r.stats, &cp)
	return nil
}

func (r *InMemRepository) Query(_ context.Context, domain string, tr TimeRange, page Pagination) ([]*Stat, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	filtered := r.filter(domain, tr)

	// Apply pagination.
	start := page.Offset
	if start > len(filtered) {
		return nil, nil
	}
	end := min(start+page.Limit, len(filtered))
	return filtered[start:end], nil
}

func (r *InMemRepository) Count(_ context.Context, domain string, tr TimeRange) (int64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	return int64(len(r.filter(domain, tr))), nil
}

func (r *InMemRepository) QueryTimeline(_ context.Context, domain string, tr TimeRange) ([]*AggregatedStat, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	filtered := r.filter(domain, tr)

	type key struct {
		Type Type
		Hour time.Time
	}
	buckets := make(map[key]int64)
	for _, s := range filtered {
		k := key{
			Type: s.Type,
			Hour: s.Timestamp.Truncate(time.Hour),
		}
		buckets[k]++
	}

	result := make([]*AggregatedStat, 0, len(buckets))
	for k, count := range buckets {
		result = append(result, &AggregatedStat{
			Type:      k.Type,
			Timestamp: k.Hour,
			Count:     count,
		})
	}

	sort.Slice(result, func(i, j int) bool {
		if result[i].Timestamp.Equal(result[j].Timestamp) {
			return result[i].Type < result[j].Type
		}
		return result[i].Timestamp.Before(result[j].Timestamp)
	})

	return result, nil
}

func (r *InMemRepository) DeleteOlderThan(_ context.Context, before time.Time) (int64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	var kept []*Stat
	var deleted int64
	for _, s := range r.stats {
		if s.Timestamp.Before(before) {
			deleted++
		} else {
			kept = append(kept, s)
		}
	}
	r.stats = kept
	return deleted, nil
}

// filter returns stats matching the domain and time range. Must be called with mu held.
func (r *InMemRepository) filter(domain string, tr TimeRange) []*Stat {
	var result []*Stat
	for _, s := range r.stats {
		if s.Domain != domain {
			continue
		}
		if s.Timestamp.Before(tr.Start) || !s.Timestamp.Before(tr.Stop) {
			continue
		}
		result = append(result, s)
	}
	return result
}
