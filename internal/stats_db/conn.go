package sq

import (
	"database/sql"
	"fmt"
	"os"

	"github.com/jackc/pgx/v4"
	"github.com/jackc/pgx/v4/stdlib"
)

func Conn() (*sql.DB, error) {
	url := os.Getenv("STATS_DATABASE_URL")
	return conn(url)
}

func conn(url string) (*sql.DB, error) {
	c, err := pgx.ParseConfig(url)
	if err != nil {
		return nil, fmt.Errorf("parsing postgres URI: %w", err)
	}

	db := stdlib.OpenDB(*c)

	return db, nil
}
