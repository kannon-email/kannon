package main

import (
	"database/sql"
	"net"
	"os"

	"github.com/joho/godotenv"
	log "github.com/sirupsen/logrus"
	"google.golang.org/grpc"
	"kannon.gyozatech.dev/generated/pb"
)

func main() {
	log.SetFormatter(&log.JSONFormatter{})
	runGrpcServer()
}

func runGrpcServer() error {
	godotenv.Load()

	dbi, err := sql.Open("postgres", os.Getenv("DB_CONN"))
	if err != nil {
		panic(err)
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
	pb.RegisterApiServer(s, apiService)

	log.Infof("🚀 starting gRPC... Listening on %v\n", lis.Addr())
	if err := s.Serve(lis); err != nil {
		return err
	}

	return nil
}
