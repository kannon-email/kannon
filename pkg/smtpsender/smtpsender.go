package smtpsender

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"time"

	msgtypes "github.com/kannon-email/kannon/proto/kannon/mailer/types"
	statstypes "github.com/kannon-email/kannon/proto/kannon/stats/types"

	"github.com/kannon-email/kannon/internal/publisher"
	"github.com/kannon-email/kannon/internal/smtp"
	"github.com/kannon-email/kannon/internal/utils"
	"github.com/kannon-email/kannon/x/container"
	"github.com/nats-io/nats.go/jetstream"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type Config struct {
	MaxJobs uint `mapstructure:"max_jobs"`
}

func (c *Config) setDefaults() {
	if c.MaxJobs == 0 {
		c.MaxJobs = 10
	}
}

type smtpSender struct {
	sender    smtp.Sender
	publisher publisher.Publisher
	js        jetstream.JetStream
	cfg       Config
}

// New constructs the SMTPSender runnable, loading its slice of configuration
// from viper under the "sender" key.
func New(cnt *container.Container) container.Runnable {
	var cfg Config
	container.LoadConfig("sender", &cfg)
	cfg.setDefaults()
	s := NewSMTPSender(cnt.NatsPublisher(), cnt.NatsJetStream(), cnt.Sender(), cfg)
	return container.Runnable{
		Name: "smtpsender",
		Run:  s.Run,
	}
}

func NewSMTPSender(publisher publisher.Publisher, js jetstream.JetStream, s smtp.Sender, cfg Config) *smtpSender {
	return &smtpSender{
		sender:    s,
		publisher: publisher,
		js:        js,
		cfg:       cfg,
	}
}

func (s *smtpSender) Run(ctx context.Context) error {
	slog.With("hostname", s.sender.SenderName(), "max_jobs", s.cfg.MaxJobs).
		Info("Starting SMTPSender Service")
	mustConfigureStatsJS(ctx, s.js)

	consumer := utils.MustGetPullSubscriber(ctx, s.js, "kannon-sending", "kannon.sending", "kannon-sending-pool")

	return s.handleSend(ctx, consumer)
}

func (s *smtpSender) handleSend(ctx context.Context, consumer jetstream.Consumer) error {
	slog.Info("🚀 Ready to send!\n")

	maxJobs := s.cfg.MaxJobs

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

	slog.Info("👋 Shutting down SMTPSender Service")
	return ctx.Err()
}

func (s *smtpSender) handleMsgAck(msg jetstream.Msg, err error) {
	if err != nil {
		slog.Error("error in handling message", "err", err)
		if err := msg.Nak(); err != nil {
			slog.Error("cannot nak message", "err", err)
		}
		return
	}
	if err := msg.Ack(); err != nil {
		slog.Error("cannot ack message", "err", err)
	}
}

func (s *smtpSender) handleMessage(msg jetstream.Msg) error {
	data := &msgtypes.EmailToSend{}
	err := proto.Unmarshal(msg.Data(), data)
	if err != nil {
		return err
	}
	sendErr := s.sender.Send(data.ReturnPath, data.To, data.Body)
	if sendErr != nil {
		slog.Info(fmt.Sprintf("Cannot send email %v - %v: %v", utils.ObfuscateEmail(data.To), data.EmailId, sendErr.Error()))
		return s.handleSendError(sendErr, data)
	}
	slog.Info(fmt.Sprintf("Email delivered: %v - %v", utils.ObfuscateEmail(data.To), data.EmailId))
	return s.handleSendSuccess(data)
}

func (s *smtpSender) handleSendSuccess(data *msgtypes.EmailToSend) error {
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

func (s *smtpSender) handleSendError(sendErr smtp.SenderError, data *msgtypes.EmailToSend) error {
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
		slog.Error("cannot create js stream", "err", err)
		os.Exit(1)
	}

	slog.Info(fmt.Sprintf("created js stream: %v", name))
}
