package sqlc

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/kannon-email/kannon/internal/stats"
	"github.com/kannon-email/kannon/internal/values"
)

// StatsRepository implements stats.Repository using sqlc queries.
type StatsRepository struct {
	db *pgxpool.Pool
}

func NewStatsRepository(db *pgxpool.Pool) *StatsRepository {
	return &StatsRepository{db: db}
}

func (r *StatsRepository) Insert(ctx context.Context, stat *stats.Stat) error {
	q := New(r.db)
	return q.InsertStat(ctx, InsertStatParams{
		Email:     stat.Email,
		MessageID: stat.MessageID,
		Type:      StatsType(stat.Type),
		Timestamp: toPgTimestamp(stat.Timestamp),
		Domain:    stat.Domain.String(),
		Data:      stat.Data,
	})
}

func (r *StatsRepository) Query(ctx context.Context, domain values.DomainName, timeRange stats.TimeRange, page stats.Pagination) ([]*stats.Stat, error) {
	q := New(r.db)
	rows, err := q.QueryStats(ctx, QueryStatsParams{
		Domain: domain.String(),
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
		stat, err := toDomainStat(row)
		if err != nil {
			return nil, err
		}
		result = append(result, stat)
	}
	return result, nil
}

func (r *StatsRepository) Count(ctx context.Context, domain values.DomainName, timeRange stats.TimeRange) (int64, error) {
	q := New(r.db)
	return q.CountQueryStats(ctx, CountQueryStatsParams{
		Domain: domain.String(),
		Start:  toPgTimestamp(timeRange.Start),
		Stop:   toPgTimestamp(timeRange.Stop),
	})
}

func (r *StatsRepository) QueryTimeline(ctx context.Context, domain values.DomainName, timeRange stats.TimeRange) ([]*stats.AggregatedStat, error) {
	q := New(r.db)
	rows, err := q.QueryStatsTimeline(ctx, QueryStatsTimelineParams{
		Domain: domain.String(),
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
	q := New(r.db)
	return q.DeleteStatsOlderThan(ctx, toPgTimestamp(before))
}

func toPgTimestamp(t time.Time) pgtype.Timestamp {
	return pgtype.Timestamp{
		Time:  t,
		Valid: true,
	}
}

// toDomainStat rebuilds the entity from its row, canonicalising the stored domain as the other row
// converters do. stats.domain is the one domain-name column with no length bound, so a row holding
// something Parse refuses is reported rather than counted against a Domain nothing can query.
func toDomainStat(row Stat) (*stats.Stat, error) {
	domain, err := values.Parse(row.Domain)
	if err != nil {
		return nil, fmt.Errorf("stat row %d holds a non-canonical domain %q: %w", row.ID, row.Domain, err)
	}
	return stats.LoadStat(
		row.ID,
		stats.Type(row.Type),
		row.Email,
		row.MessageID,
		domain,
		row.Timestamp.Time,
		row.Data,
	), nil
}
