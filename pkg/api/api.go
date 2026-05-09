package api

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"time"

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

	slog.Info(fmt.Sprintf("Starting API Service on port %d", port))

	db := cnt.DB()

	statsRepo := sq.NewStatsRepository(db)
	aggregatedRepo := sq.NewAggregatedStatsRepository(db)
	statsService := stats.NewService(statsRepo, stats.WithAggregatedStatsRepository(aggregatedRepo))

	adminAPIService := adminapi.CreateAdminAPIService(db)
	mailAPIService := mailapi.NewMailerAPIV1(db, cnt.BackoffPolicy())
	statsAPIService := statsv1.NewStatsAPIService(statsService)
	statsV2APIService := statsv2.NewStatsAPIService(statsService)
	hzAPIService := hzapi.CreateHZAPIService(cnt)

	return startAPIServer(ctx, port, adminAPIService, mailAPIService, statsAPIService, statsV2APIService, hzAPIService)
}

func startAPIServer(ctx context.Context, port uint, adminServer adminv1connect.ApiHandler, mailerServer mailerv1connect.MailerHandler, statsServer statsv1connect.StatsApiV1Handler, statsV2Server statsv2connect.StatsApiV2Handler, hzServer adminv1connect.HZServiceHandler) error {
	addr := fmt.Sprintf("0.0.0.0:%d", port)
	mux := http.NewServeMux()

	// Register Connect handlers
	adminPath, adminHandler := adminv1connect.NewApiHandler(adminServer)
	mailerPath, mailerHandler := mailerv1connect.NewMailerHandler(mailerServer)
	statsPath, statsHandler := statsv1connect.NewStatsApiV1Handler(statsServer)
	statsV2Path, statsV2Handler := statsv2connect.NewStatsApiV2Handler(statsV2Server)
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

	slog.Info(fmt.Sprintf("Connect API server listening on %s", addr))
	return server.ListenAndServe()
}
