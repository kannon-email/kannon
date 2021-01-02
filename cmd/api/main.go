package main

import (
	"net"

	"github.com/joho/godotenv"
	"github.com/sirupsen/logrus"
	log "github.com/sirupsen/logrus"
	"google.golang.org/grpc"
	"smtp.ludusrusso.space/generated/proto"
	"smtp.ludusrusso.space/internal/db"
)

func main() {
	runGrpcServer()
}

func runGrpcServer() error {
	err := godotenv.Load()
	if err != nil {
		logrus.Fatal("Error loading .env file")
	}

	dbi, err := db.NewDb(true)
	if err != nil {
		logrus.Fatalf("cannot create db: %v\n", err)
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

	log.Printf("🚀 starting gRPC... Listening on %v\n", lis.Addr())
	if err := s.Serve(lis); err != nil {
		return err
	}

	return nil
}
