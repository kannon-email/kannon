package sqlc

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/kannon-email/kannon/internal/pool"
)

// poolStatuses maps the statuses the Pool stores to the ones it reports. A row in any other status,
// such as one a legacy query left behind, is not counted.
var poolStatuses = map[SendingPoolStatus]pool.Status{
	SendingPoolStatusToValidate: pool.StatusToValidate,
	SendingPoolStatusValidating: pool.StatusValidating,
	SendingPoolStatusScheduled:  pool.StatusScheduled,
	SendingPoolStatusSending:    pool.StatusSending,
}

type poolCensus struct {
	db *pgxpool.Pool
}

// NewPoolCensus creates a PostgreSQL-backed pool.Census.
func NewPoolCensus(db *pgxpool.Pool) pool.Census {
	return &poolCensus{db: db}
}

func (c *poolCensus) DeliveriesByStatus(ctx context.Context) (map[pool.Status]int, error) {
	rows, err := New(c.db).CountPoolByStatus(ctx)
	if err != nil {
		return nil, err
	}
	counts := make(map[pool.Status]int, len(poolStatuses))
	for _, r := range rows {
		if status, ok := poolStatuses[r.Status]; ok {
			counts[status] = int(r.Deliveries)
		}
	}
	return counts, nil
}
