package sender

import (
	"context"
	"errors"
	"time"

	msgtypes "github.com/kannon-email/kannon/proto/kannon/mailer/types"
	statstypes "github.com/kannon-email/kannon/proto/kannon/stats/types"

	"github.com/kannon-email/kannon/internal/publisher"
	"github.com/kannon-email/kannon/internal/smtp"
	"github.com/kannon-email/kannon/internal/utils"
	"github.com/kannon-email/kannon/internal/x/container"
	"github.com/nats-io/nats.go"
	"github.com/sirupsen/logrus"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type Config struct {
	Hostname string
	MaxJobs  uint
}

func (c Config) GetMaxJobs() uint {
	if c.MaxJobs == 0 {
		return 10
	}
	return c.MaxJobs
}

func Run(ctx context.Context, cnt *container.Container, config Config) error {
	nc := cnt.Nats()

	senderHost := config.Hostname
	maxSendingJobs := config.GetMaxJobs()

	logrus.Infof("Starting Sender Service with hostname: %v and %d jobs", senderHost, maxSendingJobs)

	js := cnt.NatsJetStream()
	mustConfigureJS(js)

	sender := smtp.NewSender(senderHost)
	con := utils.MustGetPullSubscriber(js, "kannon.sending", "kannon-sending-pool")

	go func() {
		handleSend(sender, con, nc, maxSendingJobs)
	}()

	<-ctx.Done()

	return nil
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
	data := &msgtypes.EmailToSend{}
	err := proto.Unmarshal(msg.Data, data)
	if err != nil {
		return err
	}
	sendErr := sender.Send(data.ReturnPath, data.To, data.Body)
	if sendErr != nil {
		logrus.Infof("Cannot send email %v - %v: %v", utils.ObfuscateEmail(data.To), data.EmailId, sendErr.Error())
		return handleSendError(sendErr, data, nc)
	}
	logrus.Infof("Email delivered: %v - %v", utils.ObfuscateEmail(data.To), data.EmailId)
	return handleSendSuccess(data, nc)
}

func handleSendSuccess(data *msgtypes.EmailToSend, nc *nats.Conn) error {
	msgID, domain, err := utils.ExtractMsgIDAndDomainFromEmailID(data.EmailId)
	if err != nil {
		return nil
	}

	msg := &statstypes.Stats{
		MessageId: msgID,
		Domain:    domain,
		Email:     data.To,
		Timestamp: timestamppb.Now(),
		Data: &statstypes.StatsData{
			Data: &statstypes.StatsData_Delivered{
				Delivered: &statstypes.StatsDataDelivered{},
			},
		},
	}
	rm, err := proto.Marshal(msg)
	if err != nil {
		return err
	}
	err = nc.Publish("kannon.stats.delivered", rm)
	if err != nil {
		return err
	}
	return nil
}

func handleSendError(sendErr smtp.SenderError, data *msgtypes.EmailToSend, nc *nats.Conn) error {
	msgID, domain, err := utils.ExtractMsgIDAndDomainFromEmailID(data.EmailId)
	if err != nil {
		return nil
	}

	msg := &statstypes.Stats{
		MessageId: msgID,
		Domain:    domain,
		Email:     data.To,
		Timestamp: timestamppb.Now(),
	}
	if !data.ShouldRetry || sendErr.IsPermanent() {
		msg.Data = &statstypes.StatsData{
			Data: &statstypes.StatsData_Bounced{
				Bounced: &statstypes.StatsDataBounced{
					Permanent: sendErr.IsPermanent(),
					Code:      sendErr.Code(),
					Msg:       sendErr.Error(),
				},
			},
		}
	} else {
		msg.Data = &statstypes.StatsData{
			Data: &statstypes.StatsData_Error{
				Error: &statstypes.StatsDataError{
					Code: sendErr.Code(),
					Msg:  sendErr.Error(),
				},
			},
		}
	}

	return publisher.PublishStat(nc, msg)
}

func mustConfigureJS(js nats.JetStreamContext) {
	confs := nats.StreamConfig{
		Name:        "kannon-stats",
		Description: "Email Stats for Kannon",
		Replicas:    1,
		Subjects:    []string{"kannon.stats.*"},
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
	logrus.Infof("created js stream: %v", info.Config.Name)
}
