package tests

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/moby/moby/api/types/container"
	"github.com/ory/dockertest/v4"
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
			slog.Warn(fmt.Sprintf("connection error: %v", err))
			return err
		}
		return db.Ping(ctx)
	}); err != nil {
		return nil, nil, fmt.Errorf("cannot connect to docker: %w", err)
	}

	if err := applySchema(ctx, resource.GetPort("5432/tcp"), schema); err != nil {
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

func applySchema(ctx context.Context, dbPort, schema string) error {
	pgConnString := fmt.Sprintf("postgresql://test:test@%v:%v/test", getDBHost(), dbPort)
	conn, err := pgx.Connect(ctx, pgConnString)
	if err != nil {
		return err
	}
	defer conn.Close(ctx)

	_, err = conn.PgConn().Exec(ctx, schema).ReadAll()
	return err
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
