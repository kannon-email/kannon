package main

import (
	"context"
	"fmt"
	"os"

	"github.com/golang/protobuf/proto"
	"github.com/joho/godotenv"
	"github.com/nats-io/jsm.go"
	"github.com/nats-io/nats.go"
	"github.com/sirupsen/logrus"
	pb "kannon.gyozatech.dev/generated/proto"
	"kannon.gyozatech.dev/internal/smtp"
)

func main() {
	godotenv.Load()
	senderHost := os.Getenv("SENDER_HOST")
	if senderHost == "" {
		panic("no sender host variable")
	}

	nc, err := nats.Connect(nats.DefaultURL, nats.UseOldRequestStyle())
	if err != nil {
		logrus.Fatalf("Cannot connect to nats: %v\n", err)
	}
	mgr, err := jsm.New(nc)
	if err != nil {
		panic(err)
	}

	con, err := mgr.LoadConsumer("kannon", "sending-pool")
	if err != nil {
		panic(err)
	}
	fmt.Printf("Pull mode: %v\n", con.IsPullMode())

	sender := smtp.NewSender(senderHost)

	for {
		msg, err := con.NextMsgContext(context.Background())
		if err != nil {
			panic(err)
		}
		data := pb.EmailToSend{}
		err = proto.Unmarshal(msg.Data, &data)
		if err != nil {
			// TODO handle error
		}
		sender.Send(data.From, data.To, data.Body)
		msg.Ack()
	}
}
