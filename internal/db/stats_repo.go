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
	data := StatsDataFromOutcome(stat.Outcome)
	data.TrackingMode = stat.TrackingMode
	return q.InsertStat(ctx, InsertStatParams{
		Email:     stat.Email,
		MessageID: stat.MessageID,
		Type:      StatsType(stat.Type),
		Timestamp: toPgTimestamp(stat.Timestamp),
		Domain:    stat.Domain.String(),
		Data:      data,
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
		row.Data.Outcome(),
	), nil
}

// QueryEvents uses keyset pagination and never joins expiring operational history.
func (r *StatsRepository) QueryEvents(ctx context.Context, d values.DomainName, q stats.EventQuery, after time.Time, id int32) ([]*stats.Stat, error) {
	types := []string{"accepted", "rejected", "delivered", "bounced", "failed", "opened", "clicked"}
	if len(q.Types) > 0 {
		types = nil
		for _, t := range q.Types {
			types = append(types, string(t))
		}
	}
	rows, err := r.db.Query(ctx, `SELECT id,type,email,message_id,domain,timestamp,data FROM stats WHERE domain=$1 AND timestamp >= $2 AND timestamp < $3 AND type::text=ANY($4::text[]) AND ($5='' OR message_id=$5) AND ($6::int=0 OR (timestamp,id)<($7,$6)) AND NOT (type::text IN ('opened','clicked') AND (email = '' OR email = 'anonymous@track.' || domain OR COALESCE(data->>'trackingMode','') = 'anonymous')) ORDER BY timestamp DESC,id DESC LIMIT $8`, d.String(), q.Range.Start.UTC(), q.Range.Stop.UTC(), types, q.BatchID, id, after.UTC(), q.Limit+1)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []*stats.Stat{}
	for rows.Next() {
		var row Stat
		if err := rows.Scan(&row.ID, &row.Type, &row.Email, &row.MessageID, &row.Domain, &row.Timestamp, &row.Data); err != nil {
			return nil, err
		}
		s, err := toDomainStat(row)
		if err != nil {
			return nil, err
		}
		s.TrackingMode = row.Data.TrackingMode
		result = append(result, s)
	}
	return result, rows.Err()
}
