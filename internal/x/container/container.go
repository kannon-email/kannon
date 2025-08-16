package container

import (
	"context"
	"fmt"
	"time"

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
	hzs     []HZ
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

		c.addHZ("db", func(ctx context.Context) error {
			return db.Ping(ctx)
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

		c.addHZ("nats", func(ctx context.Context) error {
			// Check connection status
			s := nc.Status()
			if s != nats.CONNECTED {
				return fmt.Errorf("nats status is %s", s)
			}

			// Test actual connectivity with RTT
			rtt, err := nc.RTT()
			if err != nil {
				return fmt.Errorf("nats RTT check failed: %w", err)
			}

			// Check if RTT is within acceptable limits (5 seconds threshold)
			if rtt > 5*time.Second {
				return fmt.Errorf("nats RTT too high: %v (threshold: 5s)", rtt)
			}

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

	// Add JetStream health check
	c.addHZ("jetstream", func(ctx context.Context) error {
		// Check account info to verify JetStream is available
		accountInfo, err := js.AccountInfo(ctx)
		if err != nil {
			return fmt.Errorf("jetstream account info failed: %w", err)
		}

		// Check if we're approaching stream limits
		if accountInfo.Limits.MaxStreams > 0 {
			usage := float64(accountInfo.Streams) / float64(accountInfo.Limits.MaxStreams)
			if usage > 0.9 { // Alert at 90% usage
				return fmt.Errorf("jetstream stream usage high: %d/%d (%.1f%%)",
					accountInfo.Streams, accountInfo.Limits.MaxStreams, usage*100)
			}
		}

		// Check if we're approaching memory limits
		if accountInfo.Limits.MaxMemory > 0 {
			usage := float64(accountInfo.Memory) / float64(accountInfo.Limits.MaxMemory)
			if usage > 0.9 { // Alert at 90% usage
				return fmt.Errorf("jetstream memory usage high: %d/%d bytes (%.1f%%)",
					accountInfo.Memory, accountInfo.Limits.MaxMemory, usage*100)
			}
		}

		return nil
	})

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
