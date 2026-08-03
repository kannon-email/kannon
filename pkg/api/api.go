package api

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"connectrpc.com/connect"
	"github.com/kannon-email/kannon/internal/admintoken"
	"github.com/kannon-email/kannon/internal/authzconnect"
	sq "github.com/kannon-email/kannon/internal/db"
	"github.com/kannon-email/kannon/internal/stats"
	"github.com/kannon-email/kannon/pkg/api/adminapi"
	"github.com/kannon-email/kannon/pkg/api/hzapi"
	"github.com/kannon-email/kannon/pkg/api/mailapi"
	"github.com/kannon-email/kannon/pkg/statsapi/statsv1"
	"github.com/kannon-email/kannon/pkg/statsapi/statsv2"
	adminv1connect "github.com/kannon-email/kannon/proto/kannon/admin/apiv1/apiv1connect"
	mailerv1connect "github.com/kannon-email/kannon/proto/kannon/mailer/apiv1/apiv1connect"
	statsv1connect "github.com/kannon-email/kannon/proto/kannon/stats/apiv1/apiv1connect"
	statsv2connect "github.com/kannon-email/kannon/proto/kannon/stats/apiv2/apiv2connect"
	"github.com/kannon-email/kannon/x/container"
)

type Config struct {
	Port uint `mapstructure:"port"`
}

func (c *Config) setDefaults() {
	if c.Port == 0 {
		c.Port = 50051
	}
}

// AdminToken resolves the credential that authenticates the Admin API and both Stats API versions.
// Exported so the boot path can refuse to start a process asked to serve them without one, rather
// than let it come up and answer every request with unauthenticated (ADR 0009).
func AdminToken() (admintoken.Token, error) {
	t, err := admintoken.Parse(container.APIAdminToken())
	if err != nil {
		return admintoken.Token{}, fmt.Errorf(
			"the API needs an admin token to authenticate the Admin and Stats APIs: set %q in the config file, or %s in the environment (%w)",
			container.APIAdminTokenKey, container.APIAdminTokenEnvVar, err)
	}
	return t, nil
}

// New constructs the API runnable, loading its slice of configuration from
// viper under the "api" key.
func New(cnt *container.Container) container.Runnable {
	var cfg Config
	container.LoadConfig("api", &cfg)
	cfg.setDefaults()
	return container.Runnable{
		Name: "api",
		Run: func(ctx context.Context) error {
			return run(ctx, cfg, cnt)
		},
	}
}

func run(ctx context.Context, config Config, cnt *container.Container) error {
	port := config.Port

	// Resolved before the listener opens, and again here rather than only in the boot path: a
	// caller registering this runnable directly — the e2e suite does — must not be able to
	// stand up an Admin API whose credential nobody checked.
	adminToken, err := AdminToken()
	if err != nil {
		return err
	}

	slog.Info(fmt.Sprintf("Starting API Service on port %d", port))

	db := cnt.DB()

	statsRepo := sq.NewStatsRepository(db)
	aggregatedRepo := sq.NewAggregatedStatsRepository(db)
	statsService := stats.NewService(statsRepo, stats.WithAggregatedStatsRepository(aggregatedRepo))

	adminAPIService := adminapi.CreateAdminAPIService(db)
	mailAPIService := mailapi.NewMailerAPIV1(db, cnt.BackoffPolicy(), cnt.RetryWindow())
	statsAPIService := statsv1.NewStatsAPIService(statsService)
	statsV2APIService := statsv2.NewStatsAPIService(statsService)
	hzAPIService := hzapi.CreateHZAPIService(cnt)

	// The operator's credential is read here, once, and handed down. Nothing beneath
	// this point asks the configuration layer what authority a request has.
	adminAuth := authzconnect.AdminTokenHandlerOptions(adminToken)

	return startAPIServer(ctx, port, adminAuth, adminAPIService, mailAPIService, statsAPIService, statsV2APIService, hzAPIService)
}

// startAPIServer mounts every handler. adminAuth authenticates the three surfaces that answer to
// the operator's admin token. The mailer handler must never get it — it authenticates its own
// sender credential — and neither must health, which discloses nothing and is polled unauthenticated.
func startAPIServer(ctx context.Context, port uint, adminAuth []connect.HandlerOption, adminServer adminv1connect.ApiHandler, mailerServer mailerv1connect.MailerHandler, statsServer statsv1connect.StatsApiV1Handler, statsV2Server statsv2connect.StatsApiV2Handler, hzServer adminv1connect.HZServiceHandler) error {
	addr := fmt.Sprintf("0.0.0.0:%d", port)
	mux := http.NewServeMux()

	adminPath, adminHandler := adminv1connect.NewApiHandler(adminServer, adminAuth...)
	mailerPath, mailerHandler := mailerv1connect.NewMailerHandler(mailerServer)
	statsPath, statsHandler := statsv1connect.NewStatsApiV1Handler(statsServer, adminAuth...)
	statsV2Path, statsV2Handler := statsv2connect.NewStatsApiV2Handler(statsV2Server, adminAuth...)
	hzPath, hzHandler := adminv1connect.NewHZServiceHandler(hzServer)

	mux.Handle(adminPath, adminHandler)
	mux.Handle(mailerPath, mailerHandler)
	mux.Handle(statsPath, statsHandler)
	mux.Handle(statsV2Path, statsV2Handler)
	mux.Handle(hzPath, hzHandler)

	protocols := new(http.Protocols)
	protocols.SetHTTP1(true)
	protocols.SetUnencryptedHTTP2(true)

	server := &http.Server{Addr: addr, Handler: mux, Protocols: protocols}
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			slog.Error("error shutting down server", "err", err)
		}
	}()

	slog.Info("Connect API server listening on " + addr)
	return server.ListenAndServe()
}
