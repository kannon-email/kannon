package sqlc

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/kannon-email/kannon/internal/stats"
)

// StatsRepository implements stats.Repository using sqlc queries.
type StatsRepository struct {
	q *Queries
}

// NewStatsRepository creates a new StatsRepository.
func NewStatsRepository(q *Queries) *StatsRepository {
	return &StatsRepository{q: q}
}

func (r *StatsRepository) Insert(ctx context.Context, stat *stats.Stat) error {
	return r.q.InsertStat(ctx, InsertStatParams{
		Email:     stat.Email,
		MessageID: stat.MessageID,
		Type:      StatsType(stat.Type),
		Timestamp: toPgTimestamp(stat.Timestamp),
		Domain:    stat.Domain,
		Data:      stat.Data,
	})
}

func (r *StatsRepository) Query(ctx context.Context, domain string, timeRange stats.TimeRange, page stats.Pagination) ([]*stats.Stat, error) {
	rows, err := r.q.QueryStats(ctx, QueryStatsParams{
		Domain: domain,
		Start:  toPgTimestamp(timeRange.Start),
		Stop:   toPgTimestamp(timeRange.Stop),
		Skip:   int32(page.Offset),
		Take:   int32(page.Limit),
	})
	if err != nil {
		return nil, err
	}

	result := make([]*stats.Stat, 0, len(rows))
	for _, row := range rows {
		result = append(result, toDomainStat(row))
	}
	return result, nil
}

func (r *StatsRepository) Count(ctx context.Context, domain string, timeRange stats.TimeRange) (int64, error) {
	return r.q.CountQueryStats(ctx, CountQueryStatsParams{
		Domain: domain,
		Start:  toPgTimestamp(timeRange.Start),
		Stop:   toPgTimestamp(timeRange.Stop),
	})
}

func (r *StatsRepository) QueryTimeline(ctx context.Context, domain string, timeRange stats.TimeRange) ([]*stats.AggregatedStat, error) {
	rows, err := r.q.QueryStatsTimeline(ctx, QueryStatsTimelineParams{
		Domain: domain,
		Start:  toPgTimestamp(timeRange.Start),
		Stop:   toPgTimestamp(timeRange.Stop),
	})
	if err != nil {
		return nil, err
	}

	result := make([]*stats.AggregatedStat, 0, len(rows))
	for _, row := range rows {
		result = append(result, &stats.AggregatedStat{
			Type:      stats.Type(row.Type),
			Timestamp: row.Ts.Time,
			Count:     row.Count,
		})
	}
	return result, nil
}

func (r *StatsRepository) DeleteOlderThan(ctx context.Context, before time.Time) (int64, error) {
	return r.q.DeleteStatsOlderThan(ctx, toPgTimestamp(before))
}

func toPgTimestamp(t time.Time) pgtype.Timestamp {
	return pgtype.Timestamp{
		Time:  t,
		Valid: true,
	}
}

func toDomainStat(row Stat) *stats.Stat {
	return stats.LoadStat(
		row.ID,
		stats.Type(row.Type),
		row.Email,
		row.MessageID,
		row.Domain,
		row.Timestamp.Time,
		row.Data,
	)
}
