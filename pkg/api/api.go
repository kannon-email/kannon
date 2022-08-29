package api

import (
	"context"
	"fmt"
	"net"
	"sync"

	"github.com/ludusrusso/kannon/generated/pb"
	"github.com/ludusrusso/kannon/generated/pb/stats/apiv1"
	sqlc "github.com/ludusrusso/kannon/internal/db"
	sq "github.com/ludusrusso/kannon/internal/stats_db"
	"github.com/ludusrusso/kannon/pkg/api/adminapi"
	"github.com/ludusrusso/kannon/pkg/api/mailapi"
	"github.com/ludusrusso/kannon/pkg/statsapi/statsv1"
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
	mailAPIService := mailapi.NewMailAPIService(q)
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

func startAPIServer(port uint, apiServer pb.ApiServer, adminSrv pb.MailerServer, statsapiv1 apiv1.StatsApiV1Server) error {
	addr := fmt.Sprintf("0.0.0.0:%d", port)
	lis, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	defer lis.Close()

	s := grpc.NewServer()
	pb.RegisterApiServer(s, apiServer)
	pb.RegisterMailerServer(s, adminSrv)
	apiv1.RegisterStatsApiV1Server(s, statsapiv1)

	if err := s.Serve(lis); err != nil {
		return err
	}
	return nil
}
