package sqlc

import (
	"database/sql"
	"fmt"
	"os"

	"github.com/jackc/pgx/v4"
	"github.com/jackc/pgx/v4/stdlib"
)

func Conn() (*sql.DB, error) {
	c, err := pgx.ParseConfig(os.Getenv("DATABASE_URL"))
	if err != nil {
		return nil, fmt.Errorf("parsing postgres URI: %w", err)
	}

	db := stdlib.OpenDB(*c)

	return db, nil
}
