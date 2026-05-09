package sqlc

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/kannon-email/kannon/internal/stats"
)

// AggregatedStatsRepository implements stats.AggregatedStatsRepository using sqlc queries.
type AggregatedStatsRepository struct {
	db *pgxpool.Pool
}

// NewAggregatedStatsRepository creates a new AggregatedStatsRepository.
func NewAggregatedStatsRepository(db *pgxpool.Pool) *AggregatedStatsRepository {
	return &AggregatedStatsRepository{db: db}
}

func (r *AggregatedStatsRepository) Increment(ctx context.Context, domain string, timestamp time.Time, statType stats.Type) error {
	q := New(r.db)
	return q.IncrementAggregatedStat(ctx, IncrementAggregatedStatParams{
		Domain:    domain,
		Timestamp: pgtype.Timestamp{Time: timestamp, Valid: true},
		Type:      StatsType(statType),
	})
}

func (r *AggregatedStatsRepository) Query(ctx context.Context, domain string, timeRange stats.TimeRange) ([]*stats.AggregatedStat, error) {
	q := New(r.db)
	rows, err := q.QueryAggregatedStats(ctx, QueryAggregatedStatsParams{
		Domain: domain,
		Start:  pgtype.Timestamp{Time: timeRange.Start, Valid: true},
		Stop:   pgtype.Timestamp{Time: timeRange.Stop, Valid: true},
	})
	if err != nil {
		return nil, err
	}

	result := make([]*stats.AggregatedStat, 0, len(rows))
	for _, row := range rows {
		result = append(result, &stats.AggregatedStat{
			Type:      stats.Type(row.Type),
			Timestamp: row.Timestamp.Time,
			Count:     row.Count,
		})
	}
	return result, nil
}
