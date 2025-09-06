package e2e_test

import (
	"context"
	"fmt"
	"log"
	"net"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/nats-io/nats.go"
	"github.com/ory/dockertest/v3"
	"github.com/ory/dockertest/v3/docker"

	schema "github.com/kannon-email/kannon/db"
)

// TestInfrastructure holds the test infrastructure resources
type TestInfrastructure struct {
	dbURL   string
	natsURL string
	apiPort uint
	cleanup []func() error
}

func (infra *TestInfrastructure) Cleanup() {
	for _, cleanFunc := range infra.cleanup {
		if err := cleanFunc(); err != nil {
			log.Printf("Error cleaning up: %v", err)
		}
	}
}

// setupTestInfrastructure sets up PostgreSQL and NATS using dockertest
func setupTestInfrastructure(ctx context.Context) (*TestInfrastructure, error) {
	infra := &TestInfrastructure{}

	pool, err := dockertest.NewPool("")
	if err != nil {
		return infra, fmt.Errorf("could not connect to docker: %w", err)
	}

	// Start PostgreSQL
	pgRes, err := createDatabase(pool)
	if err != nil {
		return infra, err
	}
	infra.cleanup = append(infra.cleanup, func() error {
		return pool.Purge(pgRes)
	})

	// Start NATS
	natsRes, err := createNats(pool)
	if err != nil {
		return infra, fmt.Errorf("could not start nats: %w", err)
	}
	infra.cleanup = append(infra.cleanup, func() error {
		return pool.Purge(natsRes)
	})

	// Get connection URLs
	dbURL := fmt.Sprintf("postgresql://test:test@localhost:%s/test?sslmode=disable", pgRes.GetPort("5432/tcp"))
	natsURL := fmt.Sprintf("nats://localhost:%s", natsRes.GetPort("4222/tcp"))

	// Wait for PostgreSQL to be ready
	var db *pgxpool.Pool
	err = pool.Retry(func() error {
		var err error
		tmpDb, err := pgxpool.New(ctx, dbURL)
		if err != nil {
			return err
		}

		if err := tmpDb.Ping(ctx); err != nil {
			tmpDb.Close()
			return err
		}

		db = tmpDb
		return nil
	})
	if err != nil {
		return infra, fmt.Errorf("could not connect to postgres: %w", err)
	}

	// Apply database schema
	err = applySchema(ctx, db)
	if err != nil {
		return infra, fmt.Errorf("could not apply schema: %w", err)
	}
	db.Close()

	// Wait for NATS to be ready with JetStream
	err = pool.Retry(func() error {
		return testNatsConnection(natsURL)
	})
	if err != nil {
		return infra, fmt.Errorf("could not connect to nats: %w", err)
	}

	// Find available port for API
	apiPort, err := findAvailablePort()
	if err != nil {
		return infra, fmt.Errorf("could not find available port: %w", err)
	}

	infra.dbURL = dbURL
	infra.natsURL = natsURL
	infra.apiPort = apiPort

	return infra, nil
}

func createDatabase(pool *dockertest.Pool) (*dockertest.Resource, error) {
	pgRes, err := pool.RunWithOptions(&dockertest.RunOptions{
		Repository: "postgres",
		Tag:        "15-alpine",
		Env: []string{
			"POSTGRES_USER=test",
			"POSTGRES_PASSWORD=test",
			"POSTGRES_DB=test",
		},
	}, func(config *docker.HostConfig) {
		config.AutoRemove = true
		config.RestartPolicy = docker.RestartPolicy{Name: "no"}
	})
	if err != nil {
		return nil, fmt.Errorf("could not start postgres: %w", err)
	}

	if err := pgRes.Expire(300); err != nil {
		return nil, fmt.Errorf("could not set postgres expiration: %w", err)
	}

	return pgRes, nil
}

func createNats(pool *dockertest.Pool) (*dockertest.Resource, error) {
	natsRes, err := pool.RunWithOptions(&dockertest.RunOptions{
		Repository:   "nats",
		Tag:          "2.10-alpine",
		Cmd:          []string{"-js", "-m", "8222"},
		ExposedPorts: []string{"4222/tcp", "8222/tcp"},
	}, func(config *docker.HostConfig) {
		config.AutoRemove = true
		config.RestartPolicy = docker.RestartPolicy{Name: "no"}
	})
	if err != nil {
		return nil, fmt.Errorf("could not start nats: %w", err)
	}

	if err := natsRes.Expire(300); err != nil {
		return nil, fmt.Errorf("could not set nats expiration: %w", err)
	}

	return natsRes, nil
}

// applySchema applies the database schema
func applySchema(ctx context.Context, db *pgxpool.Pool) error {
	// Apply the main schema
	_, err := db.Exec(ctx, schema.Schema)
	if err != nil {
		return fmt.Errorf("failed to apply main schema: %w", err)
	}

	return nil
}

// testNatsConnection tests the NATS connection and JetStream availability
func testNatsConnection(natsURL string) error {
	nc, err := nats.Connect(natsURL)
	if err != nil {
		return err
	}
	defer nc.Close()

	// Test JetStream availability
	js, err := nc.JetStream()
	if err != nil {
		return err
	}

	// Try to get account info to verify JetStream is working
	_, err = js.AccountInfo()
	if err != nil {
		return err
	}

	return nil
}

// findAvailablePort finds an available port for the API server
func findAvailablePort() (uint, error) {
	listener, err := net.Listen("tcp", ":0")
	if err != nil {
		return 0, err
	}
	defer listener.Close()

	port := listener.Addr().(*net.TCPAddr).Port
	return uint(port), nil
}

// GetDatabaseURL returns the database URL for external use
func (infra *TestInfrastructure) GetDatabaseURL() string {
	return infra.dbURL
}

// GetNatsURL returns the NATS URL for external use
func (infra *TestInfrastructure) GetNatsURL() string {
	return infra.natsURL
}

// GetAPIPort returns the API port for external use
func (infra *TestInfrastructure) GetAPIPort() uint {
	return infra.apiPort
}

// IsHealthy checks if the infrastructure is healthy
func (infra *TestInfrastructure) IsHealthy(ctx context.Context) error {
	// Check database
	db, err := pgxpool.New(ctx, infra.dbURL)
	if err != nil {
		return fmt.Errorf("failed to connect to database: %w", err)
	}
	defer db.Close()

	if err := db.Ping(ctx); err != nil {
		return fmt.Errorf("database ping failed: %w", err)
	}

	// Check NATS
	if err := testNatsConnection(infra.natsURL); err != nil {
		return fmt.Errorf("nats connection failed: %w", err)
	}

	return nil
}
