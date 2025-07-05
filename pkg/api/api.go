package api

import (
	"context"
	"fmt"
	"net"
	"sync"

	"github.com/ludusrusso/kannon/internal/x/container"
	"github.com/ludusrusso/kannon/pkg/api/adminapi"
	"github.com/ludusrusso/kannon/pkg/api/mailapi"
	"github.com/ludusrusso/kannon/pkg/statsapi/statsv1"
	adminv1 "github.com/ludusrusso/kannon/proto/kannon/admin/apiv1"
	mailerv1 "github.com/ludusrusso/kannon/proto/kannon/mailer/apiv1"
	"github.com/ludusrusso/kannon/proto/kannon/stats/apiv1"
	"github.com/sirupsen/logrus"
	"google.golang.org/grpc"
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

	wg := sync.WaitGroup{}
	wg.Add(1)

	go func() {
		err := startAPIServer(port, adminAPIService, mailAPIService, statsAPIService)
		if err != nil {
			panic("Cannot run mailer server")
		}
	}()

	wg.Wait()

	return nil
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
