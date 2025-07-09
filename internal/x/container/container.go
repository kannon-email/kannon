package container

import (
	"context"
	"sync"

	"github.com/jackc/pgx/v5/pgxpool"
	sqlc "github.com/kannon-email/kannon/internal/db"
	"github.com/kannon-email/kannon/internal/publisher"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
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
	db   *singleton[*pgxpool.Pool]
	nats *singleton[*nats.Conn]

	closers []CloserFunc
}

type CloserFunc func() error

func (c *Container) addClosers(closers ...CloserFunc) {
	c.closers = append(c.closers, closers...)
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
	return &Container{
		ctx:  ctx,
		cfg:  cfg,
		db:   &singleton[*pgxpool.Pool]{},
		nats: &singleton[*nats.Conn]{},
	}
}

// DB returns a singleton DB connection.
func (c *Container) DB() *pgxpool.Pool {
	return c.db.Get(c.ctx, func(ctx context.Context) *pgxpool.Pool {
		db, err := sqlc.Conn(c.ctx, c.cfg.DBUrl)
		if err != nil {
			logrus.Fatalf("Failed to connect to database: %v", err)
		}

		c.addClosers(func() error {
			db.Close()
			return nil
		})
		return db
	})
}

// Queries returns a singleton Queries instance.
func (c *Container) Queries() *sqlc.Queries {
	return sqlc.New(c.DB())
}

func (c *Container) Nats() *nats.Conn {
	return c.nats.Get(c.ctx, func(ctx context.Context) *nats.Conn {
		logrus.Debugf("connecting to NATS: %s", c.cfg.NatsURL)
		nc, err := nats.Connect(c.cfg.NatsURL)
		if err != nil {
			logrus.Fatalf("Failed to connect to NATS: %v", err)
		}

		c.addClosers(func() error {
			if err := nc.Drain(); err != nil {
				return err
			}
			nc.Close()
			return nil
		})
		return nc
	})
}

func (c *Container) NatsPublisher() publisher.Publisher {
	logrus.Debugf("[nats] creating publisher")
	return &publisherWithDebug{
		nc: c.Nats(),
	}
}

func (c *Container) NatsJetStream() jetstream.JetStream {
	nc := c.Nats()
	js, err := jetstream.New(nc)
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

type publisherWithDebug struct {
	nc *nats.Conn
}

func (p *publisherWithDebug) Publish(subj string, data []byte) error {
	logrus.WithField("subj", subj).Debugf("[nats] publishing message")
	return p.nc.Publish(subj, data)
}
