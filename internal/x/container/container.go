package container

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
	sqlc "github.com/kannon-email/kannon/internal/db"
	"github.com/kannon-email/kannon/internal/publisher"
	"github.com/kannon-email/kannon/internal/smtp"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/sirupsen/logrus"
)

type Config struct {
	DBUrl        string
	NatsURL      string
	SenderConfig SenderConfig
}

type SenderConfig struct {
	DemoSender bool
	Hostname   string
}

// Container implements the Dependency Injection (DI) pattern for Go.
// It centralizes the creation and management of application services and dependencies as singletons.
// This makes the codebase more modular, testable, and maintainable.
type Container struct {
	ctx context.Context
	cfg Config

	// singleton instances
	db     *singleton[*pgxpool.Pool]
	nats   *singleton[*nats.Conn]
	sender *singleton[smtp.Sender]

	closers []CloserFunc
}

// New creates a new Container with the given context and configuration.
func New(ctx context.Context, cfg Config) *Container {
	return &Container{
		ctx:    ctx,
		cfg:    cfg,
		db:     &singleton[*pgxpool.Pool]{},
		nats:   &singleton[*nats.Conn]{},
		sender: &singleton[smtp.Sender]{},
	}
}

// DB returns a singleton DB connection.
func (c *Container) DB() *pgxpool.Pool {
	return c.db.MustGet(c.ctx, func(ctx context.Context) (*pgxpool.Pool, error) {
		db, err := sqlc.Conn(c.ctx, c.cfg.DBUrl)
		if err != nil {
			return nil, err
		}

		c.addClosers(func(_ context.Context) error {
			db.Close()
			return nil
		})
		return db, nil
	})
}

// Queries returns a singleton Queries instance.
func (c *Container) Queries() *sqlc.Queries {
	return sqlc.New(c.DB())
}

func (c *Container) Nats() *nats.Conn {
	return c.nats.MustGet(c.ctx, func(ctx context.Context) (*nats.Conn, error) {
		logrus.Debugf("connecting to NATS: %s", c.cfg.NatsURL)
		nc, err := nats.Connect(c.cfg.NatsURL)
		if err != nil {
			return nil, err
		}

		c.addClosers(func(_ context.Context) error {
			if err := nc.Drain(); err != nil {
				return err
			}
			nc.Close()
			return nil
		})
		return nc, nil
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

func (c *Container) Sender() smtp.Sender {
	return c.sender.MustGet(c.ctx, func(ctx context.Context) (smtp.Sender, error) {
		sender := smtp.NewSender(c.cfg.SenderConfig.Hostname)
		if c.cfg.SenderConfig.DemoSender {
			sender = smtp.NewDemoSender(c.cfg.SenderConfig.Hostname)
		}
		return sender, nil
	})
}

type publisherWithDebug struct {
	nc *nats.Conn
}

func (p *publisherWithDebug) Publish(subj string, data []byte) error {
	logrus.WithField("subj", subj).Debugf("[nats] publishing message")
	return p.nc.Publish(subj, data)
}
