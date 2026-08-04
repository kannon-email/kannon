package container

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	sqlc "github.com/kannon-email/kannon/internal/db"
	"github.com/kannon-email/kannon/internal/delivery"
	"github.com/kannon-email/kannon/internal/publisher"
	"github.com/kannon-email/kannon/internal/smtp"
	"github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
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
	backoff         delivery.BackoffPolicy
	retryWindow     time.Duration

	// singleton instances
	db                 *singleton[*pgxpool.Pool]
	nats               *singleton[*nats.Conn]
	embeddedNatsServer *singleton[*server.Server]
	sender             *singleton[smtp.Sender]

	// mu guards closers and hzs, which are appended to from singleton factory
	// callbacks that may run concurrently across runnable goroutines.
	mu      sync.Mutex
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
		backoff:            delivery.DefaultBackoff,
		retryWindow:        delivery.DefaultRetryWindow,
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

// WithBackoff overrides the retry backoff policy. Tests use this to collapse
// the production multi-minute curve into milliseconds without mutating
// per-package internals.
func WithBackoff(p delivery.BackoffPolicy) TestOption {
	return func(c *Container) { c.backoff = p }
}

// WithRetryWindow overrides the Retry Budget. Tests shrink it in step with
// WithBackoff — the window has to be scaled by the same factor as the backoff
// base, or a collapsed curve would race through the whole budget in
// milliseconds and terminate every Delivery in the suite.
func WithRetryWindow(w time.Duration) TestOption {
	return func(c *Container) { c.retryWindow = w }
}

// NewForTest builds a Container without reading viper, applying the supplied
// options. Tests use this to wire a synthetic container without inventing
// per-package backdoors.
func NewForTest(ctx context.Context, opts ...TestOption) *Container {
	c := &Container{
		ctx:                ctx,
		backoff:            delivery.DefaultBackoff,
		retryWindow:        delivery.DefaultRetryWindow,
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
	return c.embeddedNatsServer.MustGet(c.ctx, c.startEmbeddedNatsServer)
}

func (c *Container) startEmbeddedNatsServer(context.Context) (*server.Server, error) {
	slog.Info("Starting embedded NATS server...")

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
		return nil, errors.New("NATS server not ready after 10 seconds")
	}

	slog.Info("Embedded NATS server started at " + ns.ClientURL())

	c.addClosers(func(ctx context.Context) error {
		slog.Info("Shutting down embedded NATS server...")
		ns.Shutdown()
		ns.WaitForShutdown()
		return nil
	})

	c.addHZ("embedded-nats", func(ctx context.Context) error {
		if !ns.ReadyForConnections(100 * time.Millisecond) {
			return errors.New("embedded NATS server not ready")
		}
		return nil
	})

	return ns, nil
}

func (c *Container) Nats() *nats.Conn {
	return c.nats.MustGet(c.ctx, c.connectNats)
}

// TryNats returns the NATS connection, or the error that prevented one, where Nats exits the
// process. For a caller whose use of NATS is optional — the audit trail is the first, and it is
// opt-in — because an operator who turned such a feature on must not discover that an unreachable
// NATS stops the API from opening its listener at all (#443, and the crash-loop of #365).
//
// Nats itself keeps exiting, deliberately: for every worker in Kannon a NATS it cannot reach really
// is fatal, and there is nothing for those callers to do with an error.
func (c *Container) TryNats() (*nats.Conn, error) {
	return c.nats.Get(c.ctx, c.connectNats)
}

func (c *Container) connectNats(context.Context) (*nats.Conn, error) {
	natsURL := c.natsURL
	if c.useEmbeddedNats {
		// The embedded server's failure is reported rather than exited on, so that TryNats
		// can answer for it too. Nats still exits, one frame up.
		ns, err := c.embeddedNatsServer.Get(c.ctx, c.startEmbeddedNatsServer)
		if err != nil {
			return nil, fmt.Errorf("failed to start embedded NATS server: %w", err)
		}
		natsURL = ns.ClientURL()
	}

	slog.Debug("connecting to NATS: " + natsURL)
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
}

func (c *Container) NatsPublisher() publisher.Publisher {
	slog.Debug("[nats] creating publisher")
	return &publisherWithDebug{
		nc: c.Nats(),
	}
}

func (c *Container) NatsJetStream() jetstream.JetStream {
	js, err := c.jetStream(c.Nats())
	if err != nil {
		slog.Error("Failed to create NATS JetStream", "err", err)
		os.Exit(1)
	}
	return js
}

// TryNatsJetStream is NatsJetStream without the exit, for the caller whose use of NATS is optional.
// See TryNats for why one exists: an opt-in feature must not be able to stop the API from serving.
func (c *Container) TryNatsJetStream() (jetstream.JetStream, error) {
	nc, err := c.TryNats()
	if err != nil {
		return nil, err
	}
	return c.jetStream(nc)
}

func (c *Container) jetStream(nc *nats.Conn) (jetstream.JetStream, error) {
	js, err := jetstream.New(nc)
	if err != nil {
		return nil, err
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

	return js, nil
}

// BackoffPolicy returns the canonical retry backoff policy for this Container.
// Production uses delivery.DefaultBackoff; tests may override via WithBackoff.
func (c *Container) BackoffPolicy() delivery.BackoffPolicy {
	return c.backoff
}

// RetryWindow returns the canonical Retry Budget for this Container: how long
// Kannon keeps trying to get a Delivery out, counted from the moment its Batch
// asked for it to be sent. Production uses delivery.DefaultRetryWindow; tests
// may override via WithRetryWindow.
//
// It travels with BackoffPolicy because the two are inseparable — the window
// bounds the very curve the policy draws — and, like it, is a wiring point
// rather than a viper key (ADR 0007, ADR 0001 §"No viper key").
func (c *Container) RetryWindow() time.Duration {
	return c.retryWindow
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
			slog.Debug("JetStream stream already exists: " + s.name)
			continue
		}
		slog.Info("Creating JetStream stream: " + s.name)
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
	slog.Debug("[nats] publishing message", "subj", subj)
	return p.nc.Publish(subj, data)
}
