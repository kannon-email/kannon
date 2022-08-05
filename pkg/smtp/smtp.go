package smtp

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
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
	logrus.Debugf("Mail from: %s", from)
	s.From = from
	return nil
}

func (s *Session) Rcpt(to string) error {
	s.To = to
	return nil
}

func (s *Session) Data(r io.Reader) error {
	emailmsg, err := mail.ReadMessage(r)
	if err != nil {
		return err
	}

	for h := range emailmsg.Header {
		header := emailmsg.Header.Get(h)
		logrus.Debugf("Header: %v: %v", h, header)
	}

	email, messageID, domain, found, err := utils.ParseBounceReturnPath(s.To)
	if err != nil {
		logrus.Warnf("Error parsing bounce return path: %s", err)
		return nil
	}

	if !found {
		return nil
	}
	logrus.Debugf("got bounce for %s with %s", email, messageID)

	m := &pb.SoftBounce{
		MessageId: messageID,
		Email:     email,
		From:      s.From,
		Code:      550,
		Msg:       "",
		Timestamp: timestamppb.Now(),
		Domain:    domain,
	}

	msg, err := proto.Marshal(m)
	if err != nil {
		logrus.Errorf("Cannot marshal data: %v", err)
		return nil
	}

	buf := new(bytes.Buffer)
	buf.ReadFrom(emailmsg.Body)

	logrus.Debugf("Got bounce msg: %+v", m)
	fmt.Printf("Got bounce msg: %s", buf.String())

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
	viper.SetDefault("smtp.address", ":25")
	viper.SetDefault("smtp.domain", "localhost")
	viper.SetDefault("smtp.read_timeout", "10s")
	viper.SetDefault("smtp.write_timeout", "10s")
	viper.SetDefault("smtp.max_payload", "1024kb")
	viper.SetDefault("smtp.max_recipients", 50)

	natsURL := viper.GetString("nats_url")
	addr := viper.GetString("smtp.address")
	domain := viper.GetString("smtp.domain")
	readTimeout := viper.GetDuration("smtp.read_timeout")
	writeTimeout := viper.GetDuration("smtp.write_timeout")
	maxPayload := viper.GetSizeInBytes("smtp.max_payload")
	maxRecipients := viper.GetInt("smtp.max_recipients")

	nc, js, closeNats := utils.MustGetNats(natsURL)
	mustConfigureSoftBounceJS(js)
	defer closeNats()

	be := &Backend{
		nc: nc,
	}

	s := smtp.NewServer(be)
	defer s.Close()

	s.Addr = addr
	s.Domain = domain
	s.ReadTimeout = readTimeout
	s.WriteTimeout = writeTimeout
	s.MaxMessageBytes = int(maxPayload)
	s.MaxRecipients = maxRecipients
	s.AllowInsecureAuth = true

	go func() {
		logrus.Printf("Starting server at: %v", s.Addr)
		if err := s.ListenAndServe(); err != nil {
			logrus.Fatalf("error serving: %v", err)
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
