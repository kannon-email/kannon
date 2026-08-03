package stats

import (
	"context"
	"errors"
	"time"

	"github.com/kannon-email/kannon/internal/authz"
	"github.com/kannon-email/kannon/internal/values"
)

// Service provides stats domain operations.
type Service struct {
	repo           Repository
	aggregatedRepo AggregatedStatsRepository
}

func NewService(repo Repository, opts ...ServiceOption) *Service {
	s := &Service{repo: repo}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// ServiceOption configures optional dependencies for Service.
type ServiceOption func(*Service)

// WithAggregatedStatsRepository sets the aggregated stats repository.
func WithAggregatedStatsRepository(repo AggregatedStatsRepository) ServiceOption {
	return func(s *Service) {
		s.aggregatedRepo = repo
	}
}

// InsertStat persists a new stat event. Unguarded, and not by omission: the callers are Kannon's
// own workers consuming NATS events, so there is no request and no Principal to check. Recording
// an event is not something an outside caller can ask for at all.
func (s *Service) InsertStat(ctx context.Context, stat *Stat) error {
	return s.repo.Insert(ctx, stat)
}

// QueryStats returns stats with pagination and total count. The guard protects personal data,
// which is the line ADR 0008 draws between this Resource and the counters beneath it: a row
// carries the Recipient's address and, under Full, an IP and user agent. List, since it enumerates.
func (s *Service) QueryStats(ctx context.Context, domain values.DomainName, timeRange TimeRange, page Pagination) ([]*Stat, int64, error) {
	type result struct {
		stats []*Stat
		total int64
	}

	got, err := authz.Guard(ctx, authz.List, authz.Stats(domain), func() (result, error) {
		stats, err := s.repo.Query(ctx, domain, timeRange, page)
		if err != nil {
			return result{}, err
		}

		total, err := s.repo.Count(ctx, domain, timeRange)
		if err != nil {
			return result{}, err
		}

		return result{stats: stats, total: total}, nil
	})

	return got.stats, got.total, err
}

// QueryTimeline returns aggregated stats for a time range — v1's aggregate, guarded on Stats even
// though it returns counters, because it computes them by grouping the per-Delivery rows. Read
// rather than List: a bucketed count enumerates nothing a caller could then address.
func (s *Service) QueryTimeline(ctx context.Context, domain values.DomainName, timeRange TimeRange) ([]*AggregatedStat, error) {
	return authz.Guard(ctx, authz.Read, authz.Stats(domain), func() ([]*AggregatedStat, error) {
		return s.repo.QueryTimeline(ctx, domain, timeRange)
	})
}

// ErrNoAggregatedRepo is returned when aggregated stats operations are called without a configured repository.
var ErrNoAggregatedRepo = errors.New("aggregated stats repository not configured")

// IncrementAggregatedStat increments the hourly counter for a stat type. The bucket is the UTC
// hour, the same granularity v1 reports, so a consumer can roll buckets into days of whatever
// timezone it displays — which a UTC day bucket cannot do for negative offsets.
func (s *Service) IncrementAggregatedStat(ctx context.Context, domain values.DomainName, timestamp time.Time, statType Type) error {
	if s.aggregatedRepo == nil {
		return ErrNoAggregatedRepo
	}
	truncated := timestamp.UTC().Truncate(time.Hour)
	return s.aggregatedRepo.Increment(ctx, domain, truncated, statType)
}

// QueryAggregatedStats returns v2's counters, the one read guarded on the narrower AggregatedStats
// Resource: those rows carry no personal data, which is why ADR 0008 nests them beneath Stats. The
// missing-repository check sits inside the guard, so an unauthorized caller learns nothing from it.
func (s *Service) QueryAggregatedStats(ctx context.Context, domain values.DomainName, timeRange TimeRange) ([]*AggregatedStat, error) {
	return authz.Guard(ctx, authz.Read, authz.AggregatedStats(domain), func() ([]*AggregatedStat, error) {
		if s.aggregatedRepo == nil {
			return nil, ErrNoAggregatedRepo
		}
		return s.aggregatedRepo.Query(ctx, domain, timeRange)
	})
}

// Cleanup deletes stats older than the retention duration. Unguarded for the same reason as
// InsertStat and IncrementAggregatedStat: its caller is the retention worker acting on the
// operator's configuration, not a request anybody made. No API exposes it.
func (s *Service) Cleanup(ctx context.Context, retention time.Duration) (int64, error) {
	before := time.Now().Add(-retention)
	return s.repo.DeleteOlderThan(ctx, before)
}
