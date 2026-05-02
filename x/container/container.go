package container

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	sqlc "github.com/kannon-email/kannon/internal/db"
	"github.com/kannon-email/kannon/internal/publisher"
	"github.com/kannon-email/kannon/internal/smtp"
	"github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/sirupsen/logrus"
)

type Config struct {
	DBUrl           string
	NatsURL         string
	UseEmbeddedNats bool
	SenderConfig    SenderConfig
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
	db                 *singleton[*pgxpool.Pool]
	nats               *singleton[*nats.Conn]
	embeddedNatsServer *singleton[*server.Server]
	sender             *singleton[smtp.Sender]

	closers []CloserFunc
	hzs     []HZ
}

// New creates a new Container with the given context and configuration.
func New(ctx context.Context, cfg Config) *Container {
	return &Container{
		ctx:                ctx,
		cfg:                cfg,
		db:                 &singleton[*pgxpool.Pool]{},
		nats:               &singleton[*nats.Conn]{},
		embeddedNatsServer: &singleton[*server.Server]{},
		sender:             &singleton[smtp.Sender]{},
	}
}

// DB returns a singleton DB connection.
func (c *Container) DB() *pgxpool.Pool {
	return c.db.MustGet(c.ctx, func(ctx context.Context) (*pgxpool.Pool, error) {
		db, err := sqlc.Conn(c.ctx, c.cfg.DBUrl)
		if err != nil {
			return nil, err
		}

		c.addClosers(func(ctx context.Context) error {
			done := make(chan bool, 1)
			go func() {
				db.Close()
				done <- true
			}()
			select {
			case <-done:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
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

// EmbeddedNatsServer returns a singleton embedded NATS server instance
func (c *Container) EmbeddedNatsServer() *server.Server {
	return c.embeddedNatsServer.MustGet(c.ctx, func(ctx context.Context) (*server.Server, error) {
		logrus.Info("Starting embedded NATS server...")

		opts := &server.Options{
			Host:      "127.0.0.1",
			Port:      -1, // Random available port
			JetStream: true,
			StoreDir:  "", // Use temp directory for storage
		}

		ns, err := server.NewServer(opts)
		if err != nil {
			return nil, fmt.Errorf("failed to create NATS server: %w", err)
		}

		// Start the server in a goroutine
		go ns.Start()

		// Wait for server to be ready
		if !ns.ReadyForConnections(10 * time.Second) {
			ns.Shutdown()
			return nil, fmt.Errorf("NATS server not ready after 10 seconds")
		}

		logrus.Infof("Embedded NATS server started at %s", ns.ClientURL())

		// Add closer for embedded NATS server
		c.addClosers(func(ctx context.Context) error {
			logrus.Info("Shutting down embedded NATS server...")
			ns.Shutdown()
			ns.WaitForShutdown()
			return nil
		})

		// Add health check for embedded NATS server
		c.addHZ("embedded-nats", func(ctx context.Context) error {
			if !ns.ReadyForConnections(100 * time.Millisecond) {
				return fmt.Errorf("embedded NATS server not ready")
			}
			return nil
		})

		return ns, nil
	})
}

func (c *Container) Nats() *nats.Conn {
	return c.nats.MustGet(c.ctx, func(ctx context.Context) (*nats.Conn, error) {
		// Get NATS URL - use embedded server if enabled
		natsURL := c.cfg.NatsURL
		if c.cfg.UseEmbeddedNats {
			ns := c.EmbeddedNatsServer()
			natsURL = ns.ClientURL()
		}

		logrus.Debugf("connecting to NATS: %s", natsURL)
		nc, err := nats.Connect(natsURL)
		if err != nil {
			return nil, err
		}

		c.addClosers(func(ctx context.Context) error {
			done := make(chan error, 1)
			go func() {
				done <- nc.Drain()
			}()
			defer nc.Close()
			select {
			case err := <-done:
				return err
			case <-ctx.Done():
				return ctx.Err()
			}
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
