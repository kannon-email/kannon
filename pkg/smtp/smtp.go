package smtp

import (
	"context"
	"errors"
	"io"
	"log"
	"net/mail"
	"time"

	"github.com/emersion/go-smtp"
	"github.com/ludusrusso/kannon/generated/pb"
	"github.com/ludusrusso/kannon/internal/utils"
	"github.com/nats-io/nats.go"
	"github.com/sirupsen/logrus"
	"github.com/spf13/viper"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// The Backend implements SMTP server methods.
type Backend struct {
	nc *nats.Conn
}

func (bkd *Backend) AnonymousLogin(_ *smtp.ConnectionState) (smtp.Session, error) {
	return &Session{
		nc: bkd.nc,
	}, nil
}

func (bkd *Backend) Login(_ *smtp.ConnectionState, _, _ string) (smtp.Session, error) {
	return nil, smtp.ErrAuthUnsupported
}

// A Session is returned after EHLO.
type Session struct {
	From string
	To   string
	nc   *nats.Conn
}

func (s *Session) Mail(from string, opts smtp.MailOptions) error {
	log.Println("Mail from:", from)
	s.From = from
	return nil
}

func (s *Session) Rcpt(to string) error {
	s.To = to
	return nil
}

func (s *Session) Data(r io.Reader) error {
	_, err := mail.ReadMessage(r)
	if err != nil {
		return err
	}

	email, messageID, _, found, err := utils.ParseBounceReturnPath(s.To)
	if err != nil {
		logrus.Warnf("Error parsing bounce return path: %s", err)
		return nil
	}

	if !found {
		return nil
	}

	m := &pb.SoftBounce{
		MessageId: messageID,
		Email:     email,
		From:      s.From,
		Code:      550,
		Msg:       "",
		Timestamp: timestamppb.Now(),
	}

	msg, err := proto.Marshal(m)
	if err != nil {
		logrus.Errorf("Cannot marshal data: %v", err)
		return nil
	}

	err = s.nc.Publish("kannon.stats.soft-bounce", msg)
	if err != nil {
		logrus.Errorf("Cannot publish data: %v", err)
		return nil
	}

	return nil
}

func (s *Session) Reset() {}

func (s *Session) Logout() error {
	return nil
}

func Run(ctx context.Context) {
	natsURL := viper.GetString("nats_url")
	// addr := viper.GetString("smtp.address")
	// domain := viper.GetString("smtp.domain")

	nc, js, closeNats := utils.MustGetNats(natsURL)
	mustConfigureSoftBounceJS(js)
	defer closeNats()

	be := &Backend{
		nc: nc,
	}

	s := smtp.NewServer(be)
	defer s.Close()

	s.Addr = ":1025"
	s.Domain = "localhost"
	s.ReadTimeout = 10 * time.Second
	s.WriteTimeout = 10 * time.Second
	s.MaxMessageBytes = 1024 * 1024
	s.MaxRecipients = 50
	s.AllowInsecureAuth = true

	go func() {
		log.Println("Starting server at", s.Addr)
		if err := s.ListenAndServe(); err != nil {
			log.Fatal(err)
		}
	}()

	<-ctx.Done()
}

func mustConfigureSoftBounceJS(js nats.JetStreamContext) {
	confs := nats.StreamConfig{
		Name:        "kannon-stats-soft-bounce",
		Description: "Email Soft Bounce for Kannon",
		Replicas:    1,
		Subjects:    []string{"kannon.stats.soft-bounce"},
		Retention:   nats.LimitsPolicy,
		Duplicates:  10 * time.Minute,
		MaxAge:      24 * time.Hour,
		Storage:     nats.FileStorage,
		Discard:     nats.DiscardOld,
	}
	info, err := js.AddStream(&confs)
	if errors.Is(err, nats.ErrStreamNameAlreadyInUse) {
		logrus.Infof("stream exists")
	} else if err != nil {
		logrus.Fatalf("cannot create js stream: %v", err)
	}
	logrus.Infof("created js stream: %v", info)
}
