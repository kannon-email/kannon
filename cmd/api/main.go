package main

import (
	"database/sql"
	"fmt"
	"net"
	"os"
	"sync"

	"github.com/joho/godotenv"
	log "github.com/sirupsen/logrus"
	"google.golang.org/grpc"
	"kannon.gyozatech.dev/cmd/api/admin_api"
	"kannon.gyozatech.dev/cmd/api/mail_api"
	"kannon.gyozatech.dev/generated/pb"
)

func main() {
	log.SetFormatter(&log.JSONFormatter{})
	if err := runGrpcServer(); err != nil {
		panic(err.Error())
	}
}

func runGrpcServer() error {
	_ = godotenv.Load()

	dbi, err := sql.Open("postgres", os.Getenv("DB_CONN"))
	if err != nil {
		panic(err)
	}
	defer dbi.Close()

	adminApiService, err := admin_api.CreateAdminAPIService(dbi)
	if err != nil {
		return fmt.Errorf("cannot create Admin API service: %w", err)
	}

	mailApiService, err := mail_api.NewMailApiService(dbi)
	if err != nil {
		return fmt.Errorf("cannot create Mailer API service: %w", err)
	}

	wg := sync.WaitGroup{}
	wg.Add(2)

	go func() {
		err := startApiServer(50051, adminApiService)
		if err != nil {
			panic("Cannot run api server")
		}
	}()

	go func() {
		err := startMailerServer(50052, mailApiService)
		if err != nil {
			panic("Cannot run mailer server")
		}
	}()

	wg.Wait()

	return nil
}

func startApiServer(port uint16, srv pb.ApiServer) error {
	addr := fmt.Sprintf("0.0.0.0:%d", port)
	lis, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	defer lis.Close()

	s := grpc.NewServer()
	pb.RegisterApiServer(s, srv)

	log.Infof("🚀 starting Admin API Service on %v\n", lis.Addr())
	if err := s.Serve(lis); err != nil {
		return err
	}
	return nil
}

func startMailerServer(port uint16, srv pb.MailerServer) error {
	addr := fmt.Sprintf("0.0.0.0:%d", port)
	lis, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	defer lis.Close()

	s := grpc.NewServer()
	pb.RegisterMailerServer(s, srv)

	log.Infof("🚀 starting Mailer API Service on %v\n", lis.Addr())
	if err := s.Serve(lis); err != nil {
		return err
	}
	return nil
}
