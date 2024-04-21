package api

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"sync"

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

	go func() {
		err := startAPIServer(port, adminAPIService, mailAPIService, statsAPIService)
		if err != nil {
			panic("Cannot run mailer server")
		}
	}()

	wg.Wait()
}

func startAPIServer(port uint, adminServer adminv1.ApiHandler, mailerServer mailerv1.MailerHandler, statsServer apiv1.StatsApiV1Handler) error {
	addr := fmt.Sprintf("0.0.0.0:%d", port)

	mux := http.NewServeMux()

	mux.Handle(adminv1.NewApiHandler(adminServer))
	mux.Handle(mailerv1.NewMailerHandler(mailerServer))
	mux.Handle(apiv1.NewStatsApiV1Handler(statsServer))

	ctx := context.Background()

	logrus.Infof("🚀 starting Admin API Service on %v\n", addr)

	err := http.ListenAndServe(
		addr,
		h2c.NewHandler(mux, &http2.Server{}),
	)
	log.Fatalf("listen failed: %v", err)

	<-ctx.Done()

	return nil
}
