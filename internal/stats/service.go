package stats

import (
	"context"
	"time"
)

// Service provides stats domain operations.
type Service struct {
	repo Repository
}

// NewService creates a new stats service.
func NewService(repo Repository) *Service {
	return &Service{repo: repo}
}

// InsertStat persists a new stat event.
func (s *Service) InsertStat(ctx context.Context, stat *Stat) error {
	return s.repo.Insert(ctx, stat)
}

// QueryStats returns stats with pagination and total count.
func (s *Service) QueryStats(ctx context.Context, domain string, timeRange TimeRange, page Pagination) ([]*Stat, int64, error) {
	stats, err := s.repo.Query(ctx, domain, timeRange, page)
	if err != nil {
		return nil, 0, err
	}

	total, err := s.repo.Count(ctx, domain, timeRange)
	if err != nil {
		return nil, 0, err
	}

	return stats, total, nil
}

// QueryTimeline returns aggregated stats for a time range.
func (s *Service) QueryTimeline(ctx context.Context, domain string, timeRange TimeRange) ([]*AggregatedStat, error) {
	return s.repo.QueryTimeline(ctx, domain, timeRange)
}

// Cleanup deletes stats older than the retention duration.
func (s *Service) Cleanup(ctx context.Context, retention time.Duration) (int64, error) {
	before := time.Now().Add(-retention)
	return s.repo.DeleteOlderThan(ctx, before)
}
