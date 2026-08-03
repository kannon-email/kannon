package e2e_test

import (
	"context"
	"fmt"
	"log"
	"net"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/nats-io/nats.go"
	"github.com/ory/dockertest/v4"

	schema "github.com/kannon-email/kannon/db"
)

// TestInfrastructure holds the test infrastructure resources
type TestInfrastructure struct {
	dbURL       string
	natsURL     string
	apiPort     uint
	trackerPort uint
	smtpPort    uint
	cleanup     []func() error
}

// smtpAddr is where the bounce-receiving SMTP server listens: the address a
// remote MTA would deliver a DSN to.
func (infra *TestInfrastructure) smtpAddr() string {
	return fmt.Sprintf("localhost:%d", infra.smtpPort)
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

	pool, err := dockertest.NewPool(ctx, "")
	if err != nil {
		return infra, fmt.Errorf("could not connect to docker: %w", err)
	}
	infra.cleanup = append(infra.cleanup, func() error {
		return pool.Close(ctx)
	})

	// Start PostgreSQL
	pgRes, err := createDatabase(ctx, pool)
	if err != nil {
		return infra, err
	}
	infra.cleanup = append(infra.cleanup, func() error {
		return pgRes.Close(ctx)
	})

	// Start NATS
	natsRes, err := createNats(ctx, pool)
	if err != nil {
		return infra, fmt.Errorf("could not start nats: %w", err)
	}
	infra.cleanup = append(infra.cleanup, func() error {
		return natsRes.Close(ctx)
	})

	// Get connection URLs
	dbURL := fmt.Sprintf("postgresql://test:test@localhost:%s/test?sslmode=disable", pgRes.GetPort("5432/tcp"))
	natsURL := "nats://localhost:" + natsRes.GetPort("4222/tcp")

	// Wait for PostgreSQL to be ready
	var db *pgxpool.Pool
	err = pool.Retry(ctx, 60*time.Second, func() error {
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
	err = pool.Retry(ctx, 60*time.Second, func() error {
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

	trackerPort, err := findAvailablePort()
	if err != nil {
		return infra, fmt.Errorf("could not find available port for tracker: %w", err)
	}

	smtpPort, err := findAvailablePort()
	if err != nil {
		return infra, fmt.Errorf("could not find available port for smtp: %w", err)
	}

	infra.dbURL = dbURL
	infra.natsURL = natsURL
	infra.apiPort = apiPort
	infra.trackerPort = trackerPort
	infra.smtpPort = smtpPort

	return infra, nil
}

func createDatabase(ctx context.Context, pool dockertest.ClosablePool) (dockertest.ClosableResource, error) {
	pgRes, err := pool.Run(ctx, "postgres",
		dockertest.WithTag("17-alpine"),
		dockertest.WithEnv([]string{
			"POSTGRES_USER=test",
			"POSTGRES_PASSWORD=test",
			"POSTGRES_DB=test",
		}),
		dockertest.WithoutReuse(),
	)
	if err != nil {
		return nil, fmt.Errorf("could not start postgres: %w", err)
	}

	return pgRes, nil
}

func createNats(ctx context.Context, pool dockertest.ClosablePool) (dockertest.ClosableResource, error) {
	natsRes, err := pool.Run(ctx, "nats",
		dockertest.WithTag("2.10-alpine"),
		dockertest.WithCmd([]string{"-js", "-m", "8222"}),
		dockertest.WithoutReuse(),
	)
	if err != nil {
		return nil, fmt.Errorf("could not start nats: %w", err)
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

	port := listener.Addr().(*net.TCPAddr).Port //nolint:errcheck // net.Listen("tcp") always yields *net.TCPAddr
	return uint(port), nil
}
