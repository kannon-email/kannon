package api

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/ludusrusso/kannon/internal/x/container"
	"github.com/ludusrusso/kannon/pkg/api/adminapi"
	"github.com/ludusrusso/kannon/pkg/api/mailapi"
	"github.com/ludusrusso/kannon/pkg/statsapi/statsv1"
	adminv1connect "github.com/ludusrusso/kannon/proto/kannon/admin/apiv1/apiv1connect"
	mailerv1connect "github.com/ludusrusso/kannon/proto/kannon/mailer/apiv1/apiv1connect"
	statsv1connect "github.com/ludusrusso/kannon/proto/kannon/stats/apiv1/apiv1connect"
	"github.com/sirupsen/logrus"
)

type Config struct {
	Port uint
}

func (c Config) GetPort() uint {
	if c.Port == 0 {
		return 50051
	}
	return c.Port
}

func Run(ctx context.Context, config Config, cnt *container.Container) error {
	port := config.GetPort()

	logrus.Infof("Starting API Service on port %d", port)

	q := cnt.Queries()

	adminAPIService := adminapi.CreateAdminAPIService(q)
	mailAPIService := mailapi.NewMailerAPIV1(q)
	statsAPIService := statsv1.NewStatsAPIService(q)

	return startAPIServer(ctx, port, adminAPIService, mailAPIService, statsAPIService)
}

func startAPIServer(ctx context.Context, port uint, adminServer adminv1connect.ApiHandler, mailerServer mailerv1connect.MailerHandler, statsServer statsv1connect.StatsApiV1Handler) error {
	addr := fmt.Sprintf("0.0.0.0:%d", port)
	mux := http.NewServeMux()

	// Register Connect handlers
	adminPath, adminHandler := adminv1connect.NewApiHandler(adminServer)
	mailerPath, mailerHandler := mailerv1connect.NewMailerHandler(mailerServer)
	statsPath, statsHandler := statsv1connect.NewStatsApiV1Handler(statsServer)

	mux.Handle(adminPath, adminHandler)
	mux.Handle(mailerPath, mailerHandler)
	mux.Handle(statsPath, statsHandler)

	server := &http.Server{Addr: addr, Handler: mux}
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			logrus.Errorf("error shutting down server: %v", err)
		}
	}()

	logrus.Infof("Connect API server listening on %s", addr)
	return server.ListenAndServe()
}
