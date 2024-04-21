package api

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"

	sqlc "github.com/ludusrusso/kannon/internal/db"
	"github.com/ludusrusso/kannon/pkg/api/adminapi"
	"github.com/ludusrusso/kannon/pkg/api/mailapi"
	"github.com/ludusrusso/kannon/pkg/statsapi/statsv1"
	adminv1 "github.com/ludusrusso/kannon/proto/kannon/admin/apiv1/apiv1connect"
	mailerv1 "github.com/ludusrusso/kannon/proto/kannon/mailer/apiv1/apiv1connect"
	apiv1 "github.com/ludusrusso/kannon/proto/kannon/stats/apiv1/apiv1connect"
	"github.com/sirupsen/logrus"
	"github.com/spf13/viper"
	"golang.org/x/net/http2"
	"golang.org/x/net/http2/h2c"
)

func Run(ctx context.Context) {
	dbURL := viper.GetString("database_url")
	port := viper.GetUint("api.port")

	logrus.Infof("Starting API Service on port %d", port)

	db, q, err := sqlc.Conn(ctx, dbURL)
	if err != nil {
		logrus.Fatalf("cannot connect to database: %v", err)
	}
	defer db.Close()

	adminAPIService := adminapi.CreateAdminAPIService(q)
	mailAPIService := mailapi.NewMailerAPIV1(q)
	statsAPIService := statsv1.NewStatsAPIService(q)

	wg := sync.WaitGroup{}
	wg.Add(1)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	go func() {
		s := <-c
		logrus.Infof("got signal: %v", s)
		cancel()
		wg.Done()
	}()

	err = startAPIServer(ctx, port, adminAPIService, mailAPIService, statsAPIService)
	if err != nil {
		logrus.Fatalf("error starting API server: %v", err)
	}

	wg.Wait()
}

func startAPIServer(ctx context.Context, port uint, adminServer adminv1.ApiHandler, mailerServer mailerv1.MailerHandler, statsServer apiv1.StatsApiV1Handler) error {
	addr := fmt.Sprintf("0.0.0.0:%d", port)

	mux := http.NewServeMux()

	mux.Handle(adminv1.NewApiHandler(adminServer))
	mux.Handle(mailerv1.NewMailerHandler(mailerServer))
	mux.Handle(apiv1.NewStatsApiV1Handler(statsServer))

	logrus.Infof("🚀 starting Admin API Service on %v\n", addr)

	errch := make(chan error, 1)

	go func() {
		errch <- http.ListenAndServe(
			addr,
			h2c.NewHandler(mux, &http2.Server{}),
		)
		logrus.Infof("Admin API Service stopped\n")
	}()

	select {
	case err := <-errch:
		return err
	case <-ctx.Done():
		logrus.Infof("Shutting down Admin API Service")
		return nil
	}
}
