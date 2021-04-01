package main

import (
	"net"

	"github.com/joho/godotenv"
	log "github.com/sirupsen/logrus"
	"google.golang.org/grpc"
	"kannon.gyozatech.dev/generated/proto"
	"kannon.gyozatech.dev/internal/db"
)

func main() {
	log.SetFormatter(&log.JSONFormatter{})
	runGrpcServer()
}

func runGrpcServer() error {
	godotenv.Load()

	dbi, err := db.NewDb(true)
	if err != nil {
		log.Fatalf("cannot create db: %v\n", err)
	}

	apiService, err := createAPIService(dbi)
	if err != nil {
		return err
	}

	log.Info("😃 Open TCP Connection")
	lis, err := net.Listen("tcp", "0.0.0.0:50051")
	defer lis.Close()

	if err != nil {
		return err
	}

	s := grpc.NewServer()
	proto.RegisterApiServer(s, apiService)

	log.Infof("🚀 starting gRPC... Listening on %v\n", lis.Addr())
	if err := s.Serve(lis); err != nil {
		return err
	}

	return nil
}
