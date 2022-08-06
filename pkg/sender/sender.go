package sender

import (
	"context"
	"errors"
	"time"

	"github.com/ludusrusso/kannon/generated/pb"
	"github.com/ludusrusso/kannon/internal/smtp"
	"github.com/ludusrusso/kannon/internal/utils"
	"github.com/nats-io/nats.go"
	"github.com/sirupsen/logrus"
	"github.com/spf13/viper"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func Run(ctx context.Context) {
	viper.SetDefault("sender.max_jobs", 10)

	senderHost := viper.GetString("sender.hostname")
	natsURL := viper.GetString("nats_url")
	maxSendingJobs := viper.GetUint("sender.max_jobs")

	logrus.Infof("Starting Sender Service with hostname: %v and %d jobs", senderHost, maxSendingJobs)

	nc, js, closeNats := utils.MustGetNats(natsURL)
	defer closeNats()
	mustConfigureJS(js)

	sender := smtp.NewSender(senderHost)
	con := utils.MustGetPullSubscriber(js, "kannon.sending", "kannon-sending-pool")

	go func() {
		handleSend(sender, con, nc, maxSendingJobs)
	}()

	<-ctx.Done()
}

func handleSend(sender smtp.Sender, con *nats.Subscription, nc *nats.Conn, maxParallelJobs uint) {
	logrus.Infof("🚀 Ready to send!\n")
	ch := make(chan bool, maxParallelJobs)
	for {
		msgs, err := con.Fetch(int(maxParallelJobs), nats.MaxWait(10*time.Second))
		if err != nil {
			if err != nats.ErrTimeout {
				logrus.Errorf("error fetching messages: %v", err)
			}
			continue
		}
		for _, msg := range msgs {
			ch <- true
			go func(msg *nats.Msg) {
				err = handleMessage(msg, sender, nc)
				if err != nil {
					logrus.Errorf("error in handling message: %v\n", err.Error())
				}
				if err := msg.Ack(); err != nil {
					logrus.Errorf("cannot hack message: %v\n", err.Error())
				}
				<-ch
			}(msg)
		}
	}
}

func handleMessage(msg *nats.Msg, sender smtp.Sender, nc *nats.Conn) error {
	data := pb.EmailToSend{}
	err := proto.Unmarshal(msg.Data, &data)
	if err != nil {
		return err
	}
	sendErr := sender.Send(data.ReturnPath, data.To, data.Body)
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
	err = nc.Publish("kannon.stats.delivered", msg)
	if err != nil {
		return err
	}
	return nil
}

func handleSendError(sendErr smtp.SenderError, data *pb.EmailToSend, nc *nats.Conn) error {
	msg := pb.Error{
		MessageId:   data.MessageId,
		Code:        sendErr.Code(),
		Msg:         sendErr.Error(),
		Email:       data.To,
		IsPermanent: sendErr.IsPermanent(),
		Timestamp:   timestamppb.Now(),
	}
	errMsg, err := proto.Marshal(&msg)
	if err != nil {
		return err
	}
	err = nc.Publish("kannon.stats.error", errMsg)
	if err != nil {
		return err
	}
	return nil
}

func mustConfigureJS(js nats.JetStreamContext) {
	confs := nats.StreamConfig{
		Name:        "kannon-stats-delivered",
		Description: "Email Stats for Kannon",
		Replicas:    1,
		Subjects:    []string{"kannon.stats.delivered"},
		Retention:   nats.LimitsPolicy,
		Duplicates:  10 * time.Minute,
		MaxAge:      24 * time.Hour,
		Storage:     nats.FileStorage,
		Discard:     nats.DiscardOld,
	}
	info, err := js.AddStream(&confs)
	if errors.Is(err, nats.ErrStreamNameAlreadyInUse) {
		logrus.Infof("stream exists\n")
	} else if err != nil {
		logrus.Fatalf("cannot create js stream: %v", err)
	}
	logrus.Infof("created js stream: %v", info)

	confs = nats.StreamConfig{
		Name:        "kannon-stats-error",
		Description: "Email Stats for Kannon",
		Replicas:    1,
		Subjects:    []string{"kannon.stats.error"},
		Retention:   nats.LimitsPolicy,
		Duplicates:  10 * time.Minute,
		MaxAge:      24 * time.Hour,
		Storage:     nats.FileStorage,
		Discard:     nats.DiscardOld,
	}
	info, err = js.AddStream(&confs)
	if errors.Is(err, nats.ErrStreamNameAlreadyInUse) {
		logrus.Infof("stream exists\n")
	} else if err != nil {
		logrus.Fatalf("cannot create js stream: %v", err)
	}
	logrus.Infof("created js stream: %v", info)
}
