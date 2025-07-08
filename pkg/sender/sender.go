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

type sender struct {
	sender    smtp.Sender
	publisher publisher.Publisher
	js        nats.JetStreamContext
	cfg       Config
}

func NewSenderFromContainer(cnt *container.Container, cfg Config) *sender {
	return NewSender(cnt.NatsPublisher(), cnt.NatsJetStream(), smtp.NewSender(cfg.Hostname), cfg)
}

func NewSender(publisher publisher.Publisher, js nats.JetStreamContext, s smtp.Sender, cfg Config) *sender {
	return &sender{
		sender:    s,
		publisher: publisher,
		js:        js,
		cfg:       cfg,
	}
}

func (s *sender) Run(ctx context.Context) error {
	logrus.WithField("hostname", s.cfg.Hostname).
		WithField("max_jobs", s.cfg.GetMaxJobs()).
		Infof("Starting Sender Service")
	mustConfigureJS(s.js)

	con := utils.MustGetPullSubscriber(s.js, "kannon.sending", "kannon-sending-pool")

	go func() {
		s.handleSend(con)
	}()
	<-ctx.Done()

	return nil
}

func (s *sender) handleSend(con *nats.Subscription) {
	logrus.Infof("🚀 Ready to send!\n")

	maxJobs := s.cfg.GetMaxJobs()

	ch := make(chan bool, maxJobs)
	for {
		logrus.Debugf("fetching %d messages", maxJobs)
		msgs, err := con.Fetch(int(maxJobs), nats.MaxWait(1*time.Second))
		if err != nil {
			if err != nats.ErrTimeout {
				logrus.Errorf("error fetching messages: %v", err)
				return
			}
			continue
		}
		logrus.Debugf("fetched %d messages", len(msgs))
		for _, msg := range msgs {
			ch <- true
			go func(msg *nats.Msg) {
				err = s.handleMessage(msg)
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

func (s *sender) handleMessage(msg *nats.Msg) error {
	data := &msgtypes.EmailToSend{}
	err := proto.Unmarshal(msg.Data, data)
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
