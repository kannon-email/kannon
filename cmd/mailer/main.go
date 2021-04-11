package main

import (
	"net"

	"github.com/joho/godotenv"
	"github.com/sirupsen/logrus"
	log "github.com/sirupsen/logrus"
	"google.golang.org/grpc"
	"kannon.gyozatech.dev/generated/pb"
	"kannon.gyozatech.dev/generated/sqlc"
)

func main() {
	logrus.SetLevel(log.DebugLevel)
	runGrpcServer()
}

func runGrpcServer() error {
	godotenv.Load()

	dbi, err := sqlc.Conn()
	if err != nil {
		panic(err)
	}
	defer dbi.Close()

	mailerService, err := newMailerService(dbi)
	if err != nil {
		panic(err)
	}

	log.Info("😃 Open TCP Connection")
	lis, err := net.Listen("tcp", "0.0.0.0:50051")
	if err != nil {
		panic(err)
	}
	defer lis.Close()

	s := grpc.NewServer()
	pb.RegisterMailerServer(s, mailerService)

	log.Printf("🚀 starting gRPC... Listening on %v\n", lis.Addr())
	if err := s.Serve(lis); err != nil {
		panic(err)
	}

	return nil
}
