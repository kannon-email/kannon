package sqlc

import (
	"database/sql"
	"fmt"

	"github.com/jackc/pgx/v4"
	"github.com/jackc/pgx/v4/stdlib"
)

func NewPg(url string) (*sql.DB, error) {
	c, err := pgx.ParseConfig(url)
	if err != nil {
		return nil, fmt.Errorf("parsing postgres URI: %w", err)
	}

	db := stdlib.OpenDB(*c)
	db.SetMaxIdleConns(5)
	db.SetMaxOpenConns(7)

	return db, nil
}
