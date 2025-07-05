package container

import (
	"context"
	"database/sql"
	"sync"

	sqlc "github.com/ludusrusso/kannon/internal/db"
	"github.com/ludusrusso/kannon/pkg/api/adminapi"
	"github.com/ludusrusso/kannon/pkg/api/mailapi"
	"github.com/ludusrusso/kannon/pkg/statsapi/statsv1"
	"github.com/nats-io/nats.go"
	"github.com/sirupsen/logrus"

	adminv1 "github.com/ludusrusso/kannon/proto/kannon/admin/apiv1"
	mailerv1 "github.com/ludusrusso/kannon/proto/kannon/mailer/apiv1"
	apiv1 "github.com/ludusrusso/kannon/proto/kannon/stats/apiv1"
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
	dbOnce sync.Once
	db     *sql.DB

	qOnce sync.Once
	q     *sqlc.Queries

	adminAPIOnce sync.Once
	adminAPI     adminv1.ApiServer

	mailAPIOnce sync.Once
	mailAPI     mailerv1.MailerServer

	statsAPIOnce sync.Once
	statsAPI     apiv1.StatsApiV1Server

	natsOnce sync.Once
	nats     *nats.Conn

	closers []func()
}

func (c *Container) Close() {
	for _, closer := range c.closers {
		closer()
	}
}

// New creates a new Container with the given context and configuration.
func New(ctx context.Context, cfg Config) *Container {
	return &Container{ctx: ctx, cfg: cfg}
}

// DB returns a singleton DB connection.
func (c *Container) DB() *sql.DB {
	c.dbOnce.Do(func() {
		db, _, err := sqlc.Conn(c.ctx, c.cfg.DBUrl)
		if err != nil {
			logrus.Fatalf("Failed to connect to database: %v", err)
		}
		c.db = db
		c.closers = append(c.closers, func() {
			if err := c.db.Close(); err != nil {
				logrus.Errorf("Failed to close database: %v", err)
			}
		})
	})
	return c.db
}

// Queries returns a singleton Queries instance.
func (c *Container) Queries() *sqlc.Queries {
	return sqlc.New(c.DB())
}

// AdminAPIService returns a singleton AdminAPIService.
func (c *Container) AdminAPIService() adminv1.ApiServer {
	c.adminAPIOnce.Do(func() {
		c.adminAPI = adminapi.CreateAdminAPIService(c.Queries())
	})
	return c.adminAPI
}

// MailAPIService returns a singleton MailerAPIV1.
func (c *Container) MailAPIService() mailerv1.MailerServer {
	c.mailAPIOnce.Do(func() {
		c.mailAPI = mailapi.NewMailerAPIV1(c.Queries())
	})
	return c.mailAPI
}

// StatsAPIService returns a singleton StatsAPIService.
func (c *Container) StatsAPIService() apiv1.StatsApiV1Server {
	c.statsAPIOnce.Do(func() {
		c.statsAPI = statsv1.NewStatsAPIService(c.Queries())
	})
	return c.statsAPI
}

func (c *Container) Nats() *nats.Conn {
	c.natsOnce.Do(func() {
		nc, err := nats.Connect(c.cfg.NatsURL)
		if err != nil {
			logrus.Fatalf("Failed to connect to NATS: %v", err)
		}
		c.nats = nc

		c.closers = append(c.closers, func() {
			if err := nc.Drain(); err != nil {
				logrus.Errorf("Failed to drain NATS: %v", err)
			}
		})
	})
	return c.nats
}

func (c *Container) NatsJetStream() nats.JetStreamContext {
	js, err := c.nats.JetStream()
	if err != nil {
		logrus.Fatalf("Failed to create NATS JetStream: %v", err)
	}
	return js
}
