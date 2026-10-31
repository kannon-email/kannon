package stats

import (
	"context"
	"github.com/kannon-email/kannon/internal/apperr"
	"time"

	"github.com/kannon-email/kannon/internal/authz"
	"github.com/kannon-email/kannon/internal/values"
)

var ErrAggregateQuery = apperr.New(apperr.Malformed, "invalid_query_parameter", "invalid aggregate query")

// AggregateBucket counts event occurrences in a complete half-open interval.
type AggregateBucket struct {
	Start  time.Time
	End    time.Time
	Counts map[Type]int64
}

// QueryAggregateSeries groups the independent Domain counters, including anonymous engagement.
func (s *Service) QueryAggregateSeries(ctx context.Context, domain values.DomainName, r TimeRange, group time.Duration) ([]AggregateBucket, error) {
	return authz.Guard(ctx, authz.Read, authz.AggregatedStats(domain), func() ([]AggregateBucket, error) {
		span := r.Stop.Sub(r.Start)
		if r.Start.IsZero() || !r.Stop.After(r.Start) || group < time.Hour || group%time.Hour != 0 || !r.Start.Equal(r.Start.UTC().Truncate(time.Hour)) || span%group != 0 || span/group > 1000 || !r.Start.Add(span).Equal(r.Stop) {
			return nil, ErrAggregateQuery
		}
		if s.aggregatedRepo == nil {
			return nil, ErrNoAggregatedRepo
		}
		r.Start, r.Stop = r.Start.UTC(), r.Stop.UTC()
		rows, err := s.aggregatedRepo.Query(ctx, domain, r)
		if err != nil {
			return nil, err
		}
		buckets := make([]AggregateBucket, int(span/group))
		for i := range buckets {
			start := r.Start.UTC().Add(time.Duration(i) * group)
			buckets[i] = AggregateBucket{Start: start, End: start.Add(group), Counts: map[Type]int64{}}
		}
		for _, row := range rows {
			if publicType(row.Type) && !row.Timestamp.Before(r.Start) && row.Timestamp.Before(r.Stop) {
				i := int(row.Timestamp.Sub(r.Start) / group)
				buckets[i].Counts[row.Type] += row.Count
			}
		}
		return buckets, nil
	})
}
