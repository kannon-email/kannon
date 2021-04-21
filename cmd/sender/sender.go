package main

import (
	"context"
	"flag"

	"github.com/nats-io/jsm.go"
	"github.com/nats-io/nats.go"
	"github.com/sirupsen/logrus"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
	"kannon.gyozatech.dev/generated/pb"
	"kannon.gyozatech.dev/internal/smtp"
)

func main() {
	senderHost := flag.String("sender-host", "sender.kannon.io", "Sender hostname for SMTP presentation")
	natsURL := flag.String("nasts-url", "nats", "Nats url connection")
	maxSendingJobs := flag.Uint("max-sending-jobs", 100, "Max Parallel Job for sending")

	flag.Parse()

	nc, err := nats.Connect(*natsURL, nats.UseOldRequestStyle())
	if err != nil {
		logrus.Fatalf("Cannot connect to nats: %v\n", err)
	}

	mgr, err := jsm.New(nc)
	if err != nil {
		panic(err)
	}

	sender := smtp.NewSender(*senderHost)

	con, err := mgr.LoadConsumer("kannon", "sending-pool")
	if err != nil {
		panic(err)
	}
	handleSend(sender, con, nc, *maxSendingJobs)
}

func handleSend(sender smtp.Sender, con *jsm.Consumer, nc *nats.Conn, maxParallelJobs uint) {
	logrus.Infof("🚀 Ready to send!\n")
	ch := make(chan bool, maxParallelJobs)
	for {
		msg, err := con.NextMsgContext(context.Background())
		if err != nil {
			panic(err)
		}
		ch <- true
		go func() {
			err = handleMessage(msg, sender, nc)
			if err != nil {
				logrus.Errorf("error in handling message: %v\n", err.Error())
			}
			if err := msg.Ack(); err != nil {
				logrus.Errorf("cannot hack message: %v\n", err.Error())
			}
			<-ch
		}()
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
