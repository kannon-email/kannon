package main

import (
	"net"

	log "github.com/sirupsen/logrus"
	"google.golang.org/grpc"
	"smtp.ludusrusso.space/generated/proto"
)

func main() {
	runGrpcServer()
}

func runGrpcServer() error {
	log.Info("😃 Open TCP Connection")
	lis, err := net.Listen("tcp", "0.0.0.0:50051")
	defer lis.Close()

	if err != nil {
		return err
	}

	mailerService, err := newMailerService()
	if err != nil {
		return err
	}
	defer mailerService.Close()

	s := grpc.NewServer()
	proto.RegisterMailerServer(s, mailerService)

	log.Printf("🚀 starting gRPC... Listening on %v\n", lis.Addr())
	if err := s.Serve(lis); err != nil {
		return err
	}

	return nil
}
