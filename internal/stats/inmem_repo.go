package stats

import (
	"context"
	"slices"
	"sort"
	"sync"
	"time"

	"github.com/kannon-email/kannon/internal/keyset"
	"github.com/kannon-email/kannon/internal/tracking"
	"github.com/kannon-email/kannon/internal/values"
)

// InMemRepository is an in-memory implementation of Repository for testing.
type InMemRepository struct {
	mu     sync.Mutex
	stats  []*Stat
	nextID int32
}

func NewInMemRepository() *InMemRepository {
	return &InMemRepository{nextID: 1}
}

func (r *InMemRepository) Insert(_ context.Context, stat *Stat) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	// Mirrors the unique index the real table carries: re-inserting an event
	// already stored is a no-op, not a second row.
	for _, s := range r.stats {
		if sameEvent(s, stat) {
			return nil
		}
	}

	stat.ID = r.nextID
	r.nextID++

	// Store a copy to avoid external mutation. A plain one is deep enough: an
	// Outcome is a value object of scalars, so copying the Stat copies it whole
	// — which is what the proto.Clone this replaced was for.
	cp := *stat
	r.stats = append(r.stats, &cp)
	return nil
}

// sameEvent reports whether two stats describe the same event, using the columns
// the table's unique index is built on. The payload is deliberately not part of
// the comparison: it is not part of the index either.
func sameEvent(a, b *Stat) bool {
	return a.Email == b.Email &&
		a.MessageID == b.MessageID &&
		a.Domain == b.Domain &&
		a.Type == b.Type &&
		a.Timestamp.Equal(b.Timestamp)
}

func (r *InMemRepository) Query(_ context.Context, domain values.DomainName, tr TimeRange, page Pagination) ([]*Stat, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	filtered := r.filter(domain, tr)

	start := page.Offset
	if start > len(filtered) {
		return nil, nil
	}
	end := min(start+page.Limit, len(filtered))
	return filtered[start:end], nil
}

func (r *InMemRepository) Count(_ context.Context, domain values.DomainName, tr TimeRange) (int64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	return int64(len(r.filter(domain, tr))), nil
}

func (r *InMemRepository) QueryTimeline(_ context.Context, domain values.DomainName, tr TimeRange) ([]*AggregatedStat, error) {
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
func (r *InMemRepository) filter(domain values.DomainName, tr TimeRange) []*Stat {
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

// QueryEvents mirrors the retained event query for application tests.
func (r *InMemRepository) QueryEvents(_ context.Context, domain values.DomainName, q EventQuery, page keyset.Page[EventPosition]) ([]*Stat, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	result := []*Stat{}
	for _, s := range r.filter(domain, q.Range) {
		if !publicType(s.Type) || (len(q.Types) > 0 && !slices.Contains(q.Types, s.Type)) || (q.BatchID != "" && s.MessageID != q.BatchID) {
			continue
		}
		if after := page.After; after != nil && (s.Timestamp.After(after.Timestamp) || (s.Timestamp.Equal(after.Timestamp) && s.ID >= after.ID)) {
			continue
		}
		if (s.Type == TypeOpened || s.Type == TypeClicked) && (s.TrackingMode == tracking.ModeAnonymous || tracking.NamesNobody(s.Email, domain.String())) {
			continue
		}
		cp := *s
		result = append(result, &cp)
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Timestamp.Equal(result[j].Timestamp) {
			return result[i].ID > result[j].ID
		}
		return result[i].Timestamp.After(result[j].Timestamp)
	})
	if len(result) > page.Limit {
		result = result[:page.Limit]
	}
	return result, nil
}
