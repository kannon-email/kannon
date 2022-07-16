package sq

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/jackc/pgx/v4"
	"github.com/jackc/pgx/v4/stdlib"
)

func Conn(ctx context.Context, url string) (*sql.DB, *Queries, error) {
	c, err := pgx.ParseConfig(url)
	if err != nil {
		return nil, nil, fmt.Errorf("parsing postgres URI: %w", err)
	}

	db := stdlib.OpenDB(*c)
	q, err := Prepare(ctx, db)
	if err != nil {
		return nil, nil, fmt.Errorf("cannot prepare queries: %w", err)
	}

	return db, q, nil
}
