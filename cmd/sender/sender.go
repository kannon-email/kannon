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
	"google.golang.org/protobuf/types/known/timestamppb"
	"kannon.gyozatech.dev/generated/pb"
	"kannon.gyozatech.dev/internal/smtp"
)

func main() {
	godotenv.Load()
	senderHost := os.Getenv("SENDER_HOST")
	if senderHost == "" {
		panic("no sender host variable")
	}

	natsConn := os.Getenv("NATS_CONN")
	if natsConn == "" {
		natsConn = nats.DefaultURL
	}

	nc, err := nats.Connect(natsConn, nats.UseOldRequestStyle())
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
		err = handleMessage(msg, sender, nc)
		if err != nil {
			logrus.Errorf("error in handling message: %v\n", err.Error())
		}
		msg.Ack()
	}
}

func handleMessage(msg *nats.Msg, sender smtp.Sender, nc *nats.Conn) error {
	data := pb.EmailToSend{}
	err := proto.Unmarshal(msg.Data, &data)
	if err != nil {
		return err
	}
	sendErr := sender.Send(data.From, data.To, data.Body)
	if sendErr != nil {
		logrus.Infof("Cannot send email %v - %v: %v", data.To, data.MessageId, sendErr.Error())
		return handleSendError(sendErr, &data, nc)
	}
	logrus.Infof("Email delivered: %v - %v", data.To, data.MessageId)
	return handleSendSuccess(&data, nc)
}

func handleSendSuccess(data *pb.EmailToSend, nc *nats.Conn) error {
	msgProto := pb.Delivered{
		MessageId: data.MessageId,
		Email:     data.To,
		Timestamp: timestamppb.Now(),
	}
	msg, err := proto.Marshal(&msgProto)
	if err != nil {
		return err
	}
	err = nc.Publish("emails.delivered", msg)
	if err != nil {
		return err
	}
	return nil
}

func handleSendError(sendErr smtp.SenderError, data *pb.EmailToSend, nc *nats.Conn) error {
	msg := pb.Error{
		MessageId:   data.MessageId,
		Code:        uint32(sendErr.Code()),
		Msg:         sendErr.Error(),
		Email:       data.To,
		IsPermanent: sendErr.IsPermanent(),
		Timestamp:   timestamppb.Now(),
	}
	errMsg, err := proto.Marshal(&msg)
	if err != nil {
		return err
	}
	err = nc.Publish("emails.error", errMsg)
	if err != nil {
		return err
	}
	return nil
}
