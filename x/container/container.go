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
	"github.com/spf13/viper"
)

// Container implements the Dependency Injection (DI) pattern for Go.
// It centralizes the creation and management of application services and dependencies as singletons.
// This makes the codebase more modular, testable, and maintainable.
type Container struct {
	ctx context.Context

	dbURL           string
	natsURL         string
	useEmbeddedNats bool
	senderHostname  string
	demoSender      bool

	// singleton instances
	db                 *singleton[*pgxpool.Pool]
	nats               *singleton[*nats.Conn]
	embeddedNatsServer *singleton[*server.Server]
	sender             *singleton[smtp.Sender]

	closers []CloserFunc
	hzs     []HZ
}

type senderCfg struct {
	Hostname   string `mapstructure:"hostname"`
	DemoSender bool   `mapstructure:"demo_sender"`
}

// New creates a Container that draws its cross-cutting configuration
// (database_url, nats_url, use_embedded_nats, sender.hostname, sender.demo_sender)
// from viper. ApplyDeprecatedAliases is invoked first so legacy keys promote
// onto their canonical names before any LoadConfig call observes them.
func New(ctx context.Context) *Container {
	ApplyDeprecatedAliases()

	var sc senderCfg
	LoadConfig("sender", &sc)

	return &Container{
		ctx:                ctx,
		dbURL:              viper.GetString("database_url"),
		natsURL:            viper.GetString("nats_url"),
		useEmbeddedNats:    viper.GetBool("use_embedded_nats"),
		senderHostname:     sc.Hostname,
		demoSender:         sc.DemoSender,
		db:                 &singleton[*pgxpool.Pool]{},
		nats:               &singleton[*nats.Conn]{},
		embeddedNatsServer: &singleton[*server.Server]{},
		sender:             &singleton[smtp.Sender]{},
	}
}

// TestOption configures a test container produced by NewForTest.
type TestOption func(*Container)

// WithDBURL overrides the database URL used by the test container.
func WithDBURL(url string) TestOption {
	return func(c *Container) { c.dbURL = url }
}

// WithNatsURL overrides the NATS URL used by the test container.
func WithNatsURL(url string) TestOption {
	return func(c *Container) { c.natsURL = url }
}

// WithEmbeddedNats enables the embedded NATS server in the test container.
func WithEmbeddedNats() TestOption {
	return func(c *Container) { c.useEmbeddedNats = true }
}

// WithSenderHostname overrides the SMTP sender hostname.
func WithSenderHostname(h string) TestOption {
	return func(c *Container) { c.senderHostname = h }
}

// WithDemoSender enables the demo SMTP sender.
func WithDemoSender() TestOption {
	return func(c *Container) { c.demoSender = true }
}

// NewForTest builds a Container without reading viper, applying the supplied
// options. Tests use this to wire a synthetic container without inventing
// per-package backdoors.
func NewForTest(ctx context.Context, opts ...TestOption) *Container {
	c := &Container{
		ctx:                ctx,
		db:                 &singleton[*pgxpool.Pool]{},
		nats:               &singleton[*nats.Conn]{},
		embeddedNatsServer: &singleton[*server.Server]{},
		sender:             &singleton[smtp.Sender]{},
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// DB returns a singleton DB connection.
func (c *Container) DB() *pgxpool.Pool {
	return c.db.MustGet(c.ctx, func(ctx context.Context) (*pgxpool.Pool, error) {
		db, err := sqlc.Conn(c.ctx, c.dbURL)
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

// EmbeddedNatsServer returns a singleton embedded NATS server instance.
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

		go ns.Start()

		if !ns.ReadyForConnections(10 * time.Second) {
			ns.Shutdown()
			return nil, fmt.Errorf("NATS server not ready after 10 seconds")
		}

		logrus.Infof("Embedded NATS server started at %s", ns.ClientURL())

		c.addClosers(func(ctx context.Context) error {
			logrus.Info("Shutting down embedded NATS server...")
			ns.Shutdown()
			ns.WaitForShutdown()
			return nil
		})

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
		natsURL := c.natsURL
		if c.useEmbeddedNats {
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
			s := nc.Status()
			if s != nats.CONNECTED {
				return fmt.Errorf("nats status is %s", s)
			}

			rtt, err := nc.RTT()
			if err != nil {
				return fmt.Errorf("nats RTT check failed: %w", err)
			}

			if rtt > 5*time.Second {
				return fmt.Errorf("nats RTT too high: %v (threshold: 5s)", rtt)
			}

			return nil
		})

		if c.useEmbeddedNats {
			if err := provisionEmbeddedJetStreams(nc); err != nil {
				return nil, fmt.Errorf("failed to provision JetStream streams: %w", err)
			}
		}

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

	c.addHZ("jetstream", func(ctx context.Context) error {
		accountInfo, err := js.AccountInfo(ctx)
		if err != nil {
			return fmt.Errorf("jetstream account info failed: %w", err)
		}

		if accountInfo.Limits.MaxStreams > 0 {
			usage := float64(accountInfo.Streams) / float64(accountInfo.Limits.MaxStreams)
			if usage > 0.9 {
				return fmt.Errorf("jetstream stream usage high: %d/%d (%.1f%%)",
					accountInfo.Streams, accountInfo.Limits.MaxStreams, usage*100)
			}
		}

		if accountInfo.Limits.MaxMemory > 0 {
			usage := float64(accountInfo.Memory) / float64(accountInfo.Limits.MaxMemory)
			if usage > 0.9 {
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
		sender := smtp.NewSender(c.senderHostname)
		if c.demoSender {
			sender = smtp.NewDemoSender(c.senderHostname)
		}
		return sender, nil
	})
}

// provisionEmbeddedJetStreams creates the JetStream streams Kannon's runnables
// expect (kannon-sending, kannon-stats, kannon-bounce). Called once when the
// container connects to its embedded NATS server; idempotent against an
// existing stream.
func provisionEmbeddedJetStreams(nc *nats.Conn) error {
	js, err := nc.JetStream()
	if err != nil {
		return fmt.Errorf("get JetStream context: %w", err)
	}

	streams := []struct {
		name     string
		subjects []string
	}{
		{name: "kannon-sending", subjects: []string{"kannon.sending"}},
		{name: "kannon-stats", subjects: []string{"kannon.stats.*"}},
		{name: "kannon-bounce", subjects: []string{"kannon.bounce"}},
	}

	for _, s := range streams {
		if _, err := js.StreamInfo(s.name); err == nil {
			logrus.Debugf("JetStream stream already exists: %s", s.name)
			continue
		}
		logrus.Infof("Creating JetStream stream: %s", s.name)
		if _, err := js.AddStream(&nats.StreamConfig{
			Name:     s.name,
			Subjects: s.subjects,
			Storage:  nats.FileStorage,
		}); err != nil {
			return fmt.Errorf("create stream %s: %w", s.name, err)
		}
	}

	return nil
}

type publisherWithDebug struct {
	nc *nats.Conn
}

func (p *publisherWithDebug) Publish(subj string, data []byte) error {
	logrus.WithField("subj", subj).Debugf("[nats] publishing message")
	return p.nc.Publish(subj, data)
}
