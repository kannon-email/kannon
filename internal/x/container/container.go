package container

import (
	"context"
	"database/sql"
	"sync"

	sqlc "github.com/ludusrusso/kannon/internal/db"
	"github.com/nats-io/nats.go"
	"github.com/sirupsen/logrus"
)

type Config struct {
	DBUrl   string
	NatsURL string
}

// Container implements the Dependency Injection (DI) pattern for Go.
// It centralizes the creation and management of application services and dependencies as singletons.
// This makes the codebase more modular, testable, and maintainable.
type Container struct {
	ctx context.Context
	cfg Config

	// singleton instances
	db   *singleton[*sql.DB]
	nats *singleton[*nats.Conn]

	closers []func() error
}

func (c *Container) Close() {
	for _, closer := range c.closers {
		if err := closer(); err != nil {
			logrus.Errorf("Failed to close: %v", err)
		}
	}
}

// New creates a new Container with the given context and configuration.
func New(ctx context.Context, cfg Config) *Container {
	return &Container{ctx: ctx, cfg: cfg}
}

// DB returns a singleton DB connection.
func (c *Container) DB() *sql.DB {
	return c.db.Get(c.ctx, func(ctx context.Context) *sql.DB {
		db, _, err := sqlc.Conn(c.ctx, c.cfg.DBUrl)
		if err != nil {
			logrus.Fatalf("Failed to connect to database: %v", err)
		}
		c.closers = append(c.closers, db.Close)
		return db
	})
}

// Queries returns a singleton Queries instance.
func (c *Container) Queries() *sqlc.Queries {
	return sqlc.New(c.DB())
}

func (c *Container) Nats() *nats.Conn {
	return c.nats.Get(c.ctx, func(ctx context.Context) *nats.Conn {
		nc, err := nats.Connect(c.cfg.NatsURL)
		if err != nil {
			logrus.Fatalf("Failed to connect to NATS: %v", err)
		}

		c.closers = append(c.closers, func() error {
			if err := nc.Drain(); err != nil {
				return err
			}
			nc.Close()
			return nil
		})
		return nc
	})
}

func (c *Container) NatsJetStream() nats.JetStreamContext {
	nc := c.Nats()
	js, err := nc.JetStream()
	if err != nil {
		logrus.Fatalf("Failed to create NATS JetStream: %v", err)
	}
	return js
}

type singleton[T any] struct {
	once  sync.Once
	value T
}

func (s *singleton[T]) Get(ctx context.Context, f func(ctx context.Context) T) T {
	s.once.Do(func() {
		s.value = f(ctx)
	})
	return s.value
}
