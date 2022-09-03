package api

import (
	"context"
	"fmt"
	"net"
	"sync"

	sqlc "github.com/ludusrusso/kannon/internal/db"
	sq "github.com/ludusrusso/kannon/internal/stats_db"
	"github.com/ludusrusso/kannon/pkg/api/adminapi"
	"github.com/ludusrusso/kannon/pkg/api/mailapi"
	"github.com/ludusrusso/kannon/pkg/statsapi/statsv1"
	adminv1 "github.com/ludusrusso/kannon/proto/kannon/admin/apiv1"
	mailerv1 "github.com/ludusrusso/kannon/proto/kannon/mailer/apiv1"
	"github.com/ludusrusso/kannon/proto/kannon/stats/apiv1"
	"github.com/sirupsen/logrus"
	"github.com/spf13/viper"
	"google.golang.org/grpc"
)

func Run(ctx context.Context) {
	dbURL := viper.GetString("database_url")
	sdbURL := viper.GetString("stats_database_url")
	port := viper.GetUint("api.port")

	logrus.Infof("Starting API Service on port %d", port)

	db, q, err := sqlc.Conn(ctx, dbURL)
	if err != nil {
		logrus.Fatalf("cannot connect to database: %v", err)
	}
	defer db.Close()

	sdb, sq, err := sq.Conn(ctx, sdbURL)
	if err != nil {
		logrus.Fatalf("cannot connect to database: %v", err)
	}
	defer sdb.Close()

	adminAPIService := adminapi.CreateAdminAPIService(q)
	mailAPIService := mailapi.NewMailerAPIV1(q)
	statsAPIService := statsv1.NewStatsAPIService(sq)

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

func startAPIServer(port uint, adminServer adminv1.ApiServer, mailerServer mailerv1.MailerServer, statsServer apiv1.StatsApiV1Server) error {
	addr := fmt.Sprintf("0.0.0.0:%d", port)
	lis, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	defer lis.Close()

	s := grpc.NewServer()
	adminv1.RegisterApiServer(s, adminServer)
	mailerv1.RegisterMailerServer(s, mailerServer)
	apiv1.RegisterStatsApiV1Server(s, statsServer)

	if err := s.Serve(lis); err != nil {
		return err
	}
	return nil
}
