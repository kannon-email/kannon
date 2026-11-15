package tests

import (
	"context"
	"errors"
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

// Main runs a package's TestMain body under dockertest.Wrap and exits with
// its status. Containers are then removed even when the run is interrupted
// (SIGINT/SIGTERM), and containers left behind by earlier runs that the
// -test.timeout killed are swept. Containers must be created inside run.
func Main(run func() int) {
	os.Exit(dockertest.Wrap(context.Background(), run).Run())
}

func TestPostgresInit(schema string) (*pgxpool.Pool, PurgeFunc, error) {
	if url := os.Getenv("KANNON_TEST_DATABASE_URL"); url != "" {
		return localPostgres(url, schema)
	}
	url, purge, err := TestPostgresURL()
	if err != nil {
		return nil, nil, err
	}
	ctx := context.Background()
	if err := applySchema(ctx, url, schema); err != nil {
		return nil, nil, errors.Join(fmt.Errorf("cannot apply schema: %w", err), purge())
	}
	db, err := pgxpool.New(ctx, url)
	if err != nil {
		return nil, nil, errors.Join(fmt.Errorf("cannot create pgx pool: %w", err), purge())
	}
	return db, func() error {
		db.Close()
		return purge()
	}, nil
}

// TestPostgresURL starts an empty disposable Postgres and returns its URL, for a caller that
// prepares the schema itself — the e2e suite runs the real migrations against it.
func TestPostgresURL() (string, PurgeFunc, error) {
	ctx := context.Background()

	pool, err := dockertest.NewPool(ctx, "")
	if err != nil {
		return "", nil, fmt.Errorf("cannot connect to docker: %w", err)
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
		return "", nil, fmt.Errorf("cannot start resource: %w", err)
	}

	url := fmt.Sprintf("postgresql://test:test@%v:%v/test?sslmode=disable", getDBHost(), resource.GetPort("5432/tcp"))
	if err = pool.Retry(ctx, 60*time.Second, func() error {
		db, err := pgxpool.New(ctx, url)
		if err != nil {
			slog.Warn(fmt.Sprintf("connection error: %v", err))
			return err
		}
		defer db.Close()
		return db.Ping(ctx)
	}); err != nil {
		return "", nil, fmt.Errorf("cannot connect to docker: %w", err)
	}

	return url, func() error {
		if err := resource.Close(ctx); err != nil {
			return fmt.Errorf("cannot purge resource: %w", err)
		}
		return pool.Close(ctx)
	}, nil
}

func applySchema(ctx context.Context, url, schema string) error {
	conn, err := pgx.Connect(ctx, url)
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

// localPostgres creates an isolated disposable database for each test fixture.
func localPostgres(url, schema string) (*pgxpool.Pool, PurgeFunc, error) {
	admin, err := pgxpool.New(context.Background(), url)
	if err != nil {
		return nil, nil, err
	}
	name := fmt.Sprintf("kannon_rest_%d", time.Now().UnixNano())
	if _, err = admin.Exec(context.Background(), "CREATE DATABASE "+name); err != nil {
		admin.Close()
		return nil, nil, err
	}
	config, err := pgxpool.ParseConfig(url)
	if err != nil {
		admin.Close()
		return nil, nil, err
	}
	config.ConnConfig.Database = name
	pool, err := pgxpool.NewWithConfig(context.Background(), config)
	if err != nil {
		admin.Close()
		return nil, nil, err
	}
	if _, err = pool.Exec(context.Background(), schema); err != nil {
		pool.Close()
		admin.Close()
		return nil, nil, err
	}
	if _, err = pool.Exec(context.Background(), "SET search_path TO public"); err != nil {
		return nil, nil, err
	}
	return pool, func() error {
		pool.Close()
		_, err := admin.Exec(context.Background(), "DROP DATABASE "+name)
		admin.Close()
		return err
	}, nil
}
