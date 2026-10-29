package sqlc

import (
	"context"
	"math/big"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/kannon-email/kannon/internal/stats"
	"github.com/kannon-email/kannon/internal/values"
)

// AggregatedStatsRepository implements stats.AggregatedStatsRepository using sqlc queries.
type AggregatedStatsRepository struct {
	db *pgxpool.Pool
}

func NewAggregatedStatsRepository(db *pgxpool.Pool) *AggregatedStatsRepository {
	return &AggregatedStatsRepository{db: db}
}

func (r *AggregatedStatsRepository) Increment(ctx context.Context, domain values.DomainName, timestamp time.Time, statType stats.Type) error {
	q := New(r.db)
	return q.IncrementAggregatedStat(ctx, IncrementAggregatedStatParams{
		Domain:    domain.String(),
		Timestamp: pgtype.Timestamp{Time: timestamp, Valid: true},
		Type:      StatsType(statType),
	})
}

func (r *AggregatedStatsRepository) Query(ctx context.Context, domain values.DomainName, timeRange stats.TimeRange) ([]*stats.AggregatedStat, error) {
	q := New(r.db)
	rows, err := q.QueryAggregatedStats(ctx, QueryAggregatedStatsParams{
		Domain: domain.String(),
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

// Count commits the deduplication marker and hourly increment in one SQL statement.
// A counter write failure rolls back its marker too, making redelivery retryable.
func (r *AggregatedStatsRepository) Count(ctx context.Context, id stats.StoredEventID, domain values.DomainName, timestamp time.Time, statType stats.Type) error {
	if err := id.Validate(); err != nil {
		return err
	}
	return New(r.db).CountAggregatedEvent(ctx, CountAggregatedEventParams{
		Domain:        domain.String(),
		Timestamp:     pgtype.Timestamp{Time: timestamp.UTC().Truncate(time.Hour), Valid: true},
		Type:          StatsType(statType),
		Stream:        id.Stream,
		StreamCreated: id.StreamCreated.UTC().Format(time.RFC3339Nano),
		Sequence:      pgtype.Numeric{Int: new(big.Int).SetUint64(id.Sequence), Valid: true},
	})
}
