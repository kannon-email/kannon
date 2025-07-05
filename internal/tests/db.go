package tests

import (
	"context"
	"fmt"
	"os"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jmoiron/sqlx"
	"github.com/ory/dockertest/v3"
	"github.com/ory/dockertest/v3/docker"
	"github.com/sirupsen/logrus"
)

type PurgeFunc func() error

func TestPostgresInit(schema string) (*pgxpool.Pool, PurgeFunc, error) {
	var db *pgxpool.Pool

	// uses a sensible default on windows (tcp/http) and linux/osx (socket)
	pool, err := dockertest.NewPool("")
	if err != nil {
		return nil, nil, fmt.Errorf("cannot connect to docker: %w", err)
	}

	// pulls an image, creates a container based on it and runs it
	resource, err := pool.RunWithOptions(&dockertest.RunOptions{
		Repository: "postgres",
		Tag:        "13-alpine",
		Env: []string{
			"POSTGRES_USER=test",
			"POSTGRES_PASSWORD=test",
			"listen_addresses = '*'",
		},
	}, func(config *docker.HostConfig) {
		// set AutoRemove to true so that stopped container goes away by itself
		config.AutoRemove = true
		config.RestartPolicy = docker.RestartPolicy{
			Name: "no",
		}
		config.Tmpfs = map[string]string{"/var/lib/postgres": "rw"}
	})
	if err != nil {
		return nil, nil, fmt.Errorf("cannot start resource: %w", err)
	}
	if err := resource.Expire(60); err != nil {
		return nil, nil, fmt.Errorf("cannot expire resource: %w", err)
	}

	if err = pool.Retry(func() error {
		var err error
		db, err = initDB(resource.GetPort("5432/tcp"))
		if err != nil {
			logrus.Warnf("connection error: %v", err)
			return err
		}
		return db.Ping(context.Background())
	}); err != nil {
		return nil, nil, fmt.Errorf("cannot connect to docker: %w", err)
	}

	if err := applySchema(resource.GetPort("5432/tcp"), schema); err != nil {
		return nil, nil, fmt.Errorf("cannot apply schema: %w", err)
	}

	var purgeFunc PurgeFunc = func() error {
		err := pool.Purge(resource)
		if err != nil {
			return fmt.Errorf("cannot purge resource: %w", err)
		}
		return nil
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
