package tests

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jmoiron/sqlx"
	"github.com/moby/moby/api/types/container"
	"github.com/ory/dockertest/v4"
	"github.com/sirupsen/logrus"
)

type PurgeFunc func() error

func TestPostgresInit(schema string) (*pgxpool.Pool, PurgeFunc, error) {
	var db *pgxpool.Pool
	ctx := context.Background()

	pool, err := dockertest.NewPool(ctx, "")
	if err != nil {
		return nil, nil, fmt.Errorf("cannot connect to docker: %w", err)
	}

	resource, err := pool.Run(ctx, "postgres",
		dockertest.WithTag("17-alpine"),
		dockertest.WithEnv([]string{
			"POSTGRES_USER=test",
			"POSTGRES_PASSWORD=test",
			"listen_addresses = '*'",
		}),
		dockertest.WithHostConfig(func(hc *container.HostConfig) {
			hc.Tmpfs = map[string]string{"/var/lib/postgres": "rw"}
		}),
		dockertest.WithoutReuse(),
	)
	if err != nil {
		return nil, nil, fmt.Errorf("cannot start resource: %w", err)
	}

	if err = pool.Retry(ctx, 60*time.Second, func() error {
		var err error
		db, err = initDB(resource.GetPort("5432/tcp"))
		if err != nil {
			logrus.Warnf("connection error: %v", err)
			return err
		}
		return db.Ping(ctx)
	}); err != nil {
		return nil, nil, fmt.Errorf("cannot connect to docker: %w", err)
	}

	if err := applySchema(resource.GetPort("5432/tcp"), schema); err != nil {
		return nil, nil, fmt.Errorf("cannot apply schema: %w", err)
	}

	var purgeFunc PurgeFunc = func() error {
		if err := resource.Close(ctx); err != nil {
			return fmt.Errorf("cannot purge resource: %w", err)
		}
		return pool.Close(ctx)
	}

	return db, purgeFunc, nil
}

func applySchema(dbPort string, schema string) error {
	dbHost := getDBHost()
	db, err := sqlx.Connect("postgres", fmt.Sprintf("host=%v user=test dbname=test password=test sslmode=disable port=%v", dbHost, dbPort))
	if err != nil {
		return fmt.Errorf("cannot open migration: %s", err)
	}

	if _, err := db.Exec(schema); err != nil {
		return err
	}
	return nil
}

func getDBHost() string {
	host := os.Getenv("YOUR_APP_DB_HOST")
	if host == "" {
		host = "localhost"
	}
	return host
}

func initDB(dbPort string) (*pgxpool.Pool, error) {
	dbHost := getDBHost()
	url := fmt.Sprintf("postgresql://test:test@%v:%v/test", dbHost, dbPort)

	pool, err := pgxpool.New(context.Background(), url)
	if err != nil {
		return nil, fmt.Errorf("cannot create pgx pool: %w", err)
	}
	return pool, nil
}
