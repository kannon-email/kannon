package api

import (
	"context"
	"fmt"
	"net"
	"sync"

	"github.com/ludusrusso/kannon/generated/pb"
	sqlc "github.com/ludusrusso/kannon/internal/db"
	"github.com/ludusrusso/kannon/pkg/api/adminapi"
	"github.com/ludusrusso/kannon/pkg/api/mailapi"
	"github.com/sirupsen/logrus"
	"github.com/spf13/viper"
	"google.golang.org/grpc"
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
	mailAPIService := mailapi.NewMailAPIService(q)

	wg := sync.WaitGroup{}
	wg.Add(1)

	go func() {
		err := startAPIServer(port, adminAPIService, mailAPIService)
		if err != nil {
			panic("Cannot run mailer server")
		}
	}()

	wg.Wait()
}

func startAPIServer(port uint, apiServer pb.ApiServer, adminSrv pb.MailerServer) error {
	addr := fmt.Sprintf("0.0.0.0:%d", port)
	lis, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	defer lis.Close()

	s := grpc.NewServer()
	pb.RegisterApiServer(s, apiServer)
	pb.RegisterMailerServer(s, adminSrv)

	if err := s.Serve(lis); err != nil {
		return err
	}
	return nil
}
