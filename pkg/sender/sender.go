package sender

import (
	"context"
	"fmt"
	"time"

	msgtypes "github.com/kannon-email/kannon/proto/kannon/mailer/types"
	statstypes "github.com/kannon-email/kannon/proto/kannon/stats/types"

	"github.com/kannon-email/kannon/internal/publisher"
	"github.com/kannon-email/kannon/internal/smtp"
	"github.com/kannon-email/kannon/internal/utils"
	"github.com/kannon-email/kannon/internal/x/container"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/sirupsen/logrus"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type Config struct {
	MaxJobs uint
}

func (c Config) GetMaxJobs() uint {
	if c.MaxJobs == 0 {
		return 10
	}
	return c.MaxJobs
}

type sender struct {
	sender    smtp.Sender
	publisher publisher.Publisher
	js        jetstream.JetStream
	cfg       Config
}

func NewSenderFromContainer(cnt *container.Container, cfg Config) *sender {
	sender := cnt.Sender()
	js := cnt.NatsJetStream()
	publisher := cnt.NatsPublisher()
	return NewSender(publisher, js, sender, cfg)
}

func NewSender(publisher publisher.Publisher, js jetstream.JetStream, s smtp.Sender, cfg Config) *sender {
	return &sender{
		sender:    s,
		publisher: publisher,
		js:        js,
		cfg:       cfg,
	}
}

func (s *sender) Run(ctx context.Context) error {
	logrus.WithField("hostname", s.sender.SenderName()).
		WithField("max_jobs", s.cfg.GetMaxJobs()).
		Infof("Starting Sender Service")
	mustConfigureStatsJS(ctx, s.js)

	consumer := utils.MustGetPullSubscriber(ctx, s.js, "kannon-sending", "kannon.sending", "kannon-sending-pool")

	return s.handleSend(ctx, consumer)
}

func (s *sender) handleSend(ctx context.Context, consumer jetstream.Consumer) error {
	logrus.Infof("🚀 Ready to send!\n")

	maxJobs := s.cfg.GetMaxJobs()

	tasks := NewParallel(maxJobs)

	con, err := consumer.Consume(func(msg jetstream.Msg) {
		tasks.RunTask(func() {
			err := s.handleMessage(msg)
			s.handleMsgAck(msg, err)
		})
	})
	if err != nil {
		return fmt.Errorf("error in consuming messages: %w", err)
	}
	defer con.Drain()

	<-ctx.Done()
	tasks.WaitAndClose()

	logrus.Infof("👋 Shutting down Sender Service")
	return ctx.Err()
}

func (s *sender) handleMsgAck(msg jetstream.Msg, err error) {
	if err != nil {
		logrus.Errorf("error in handling message: %v\n", err.Error())
		if err := msg.Nak(); err != nil {
			logrus.Errorf("cannot nak message: %v\n", err.Error())
		}
		return
	}
	if err := msg.Ack(); err != nil {
		logrus.Errorf("cannot ack message: %v\n", err.Error())
	}
}

func (s *sender) handleMessage(msg jetstream.Msg) error {
	data := &msgtypes.EmailToSend{}
	err := proto.Unmarshal(msg.Data(), data)
	if err != nil {
		return err
	}
	sendErr := s.sender.Send(data.ReturnPath, data.To, data.Body)
	if sendErr != nil {
		logrus.Infof("Cannot send email %v - %v: %v", utils.ObfuscateEmail(data.To), data.EmailId, sendErr.Error())
		return s.handleSendError(sendErr, data)
	}
	logrus.Infof("Email delivered: %v - %v", utils.ObfuscateEmail(data.To), data.EmailId)
	return s.handleSendSuccess(data)
}

func (s *sender) handleSendSuccess(data *msgtypes.EmailToSend) error {
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
	err = s.publisher.Publish("kannon.stats.delivered", rm)
	if err != nil {
		return err
	}
	return nil
}

func (s *sender) handleSendError(sendErr smtp.SenderError, data *msgtypes.EmailToSend) error {
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

	return publisher.PublishStat(s.publisher, msg)
}

func mustConfigureStatsJS(ctx context.Context, js jetstream.JetStream) {
	name := "kannon-stats"

	confs := jetstream.StreamConfig{
		Name:        name,
		Description: "Email Stats for Kannon",
		Replicas:    1,
		Subjects:    []string{"kannon.stats.*"},
		Retention:   jetstream.LimitsPolicy,
		Duplicates:  10 * time.Minute,
		MaxAge:      24 * time.Hour,
		Storage:     jetstream.FileStorage,
		Discard:     jetstream.DiscardOld,
	}
	_, err := js.CreateOrUpdateStream(ctx, confs)
	if err != nil {
		logrus.Fatalf("cannot create js stream: %v", err)
	}

	logrus.Infof("created js stream: %v", name)
}
