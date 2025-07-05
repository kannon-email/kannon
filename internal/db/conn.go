package sqlc

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

func Conn(ctx context.Context, url string) (*pgxpool.Pool, error) {
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		return nil, fmt.Errorf("cannot create pgx pool: %w", err)
	}

	return pool, nil
}
