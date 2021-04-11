package main

import (
	"net"

	"github.com/joho/godotenv"
	log "github.com/sirupsen/logrus"
	"google.golang.org/grpc"
	"kannon.gyozatech.dev/generated/pb"
	"kannon.gyozatech.dev/generated/sqlc"
)

func main() {
	log.SetFormatter(&log.JSONFormatter{})
	runGrpcServer()
}

func runGrpcServer() error {
	godotenv.Load()
	dbi, err := sqlc.Conn()
	if err != nil {
		panic(err)
	}
	defer dbi.Close()

	apiService, err := createAPIService(dbi)
	if err != nil {
		return err
	}

	log.Info("😃 Open TCP Connection")
	lis, err := net.Listen("tcp", "0.0.0.0:50051")
	if err != nil {
		panic(err)
	}
	defer lis.Close()

	s := grpc.NewServer()
	pb.RegisterApiServer(s, apiService)

	log.Infof("🚀 starting gRPC... Listening on %v\n", lis.Addr())
	if err := s.Serve(lis); err != nil {
		return err
	}

	return nil
}
