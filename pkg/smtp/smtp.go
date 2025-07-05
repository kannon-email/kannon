package smtp

import (
	"bufio"
	"context"
	"io"
	"net/mail"
	"regexp"
	"strconv"

	"github.com/emersion/go-smtp"
	"github.com/ludusrusso/kannon/internal/utils"
	st "github.com/ludusrusso/kannon/proto/kannon/stats/types"
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

func (bkd *Backend) NewSession(_ *smtp.Conn) (smtp.Session, error) {
	return &Session{
		nc: bkd.nc,
	}, nil
}

// A Session is returned after EHLO.
type Session struct {
	From string
	To   string
	nc   *nats.Conn
}

func (s *Session) AuthPlain(username, password string) error {
	return nil
}

func (s *Session) Mail(from string, opts *smtp.MailOptions) error {
	logrus.Debugf("Mail from: %s", from)
	s.From = from
	return nil
}

func (s *Session) Rcpt(to string, opts *smtp.RcptOptions) error {
	s.To = to
	return nil
}

func (s *Session) Data(r io.Reader) error {
	emailmsg, err := mail.ReadMessage(r)
	if err != nil {
		return err
	}

	email, messageID, domain, found, err := utils.ParseBounceReturnPath(s.To)
	if err != nil {
		logrus.Warnf("Error parsing bounce return path: %s", err)
		return nil
	}

	if !found {
		return nil
	}

	code, errMsg := parseCode(emailmsg.Body)

	m := &st.Stats{
		MessageId: messageID,
		Email:     email,
		Timestamp: timestamppb.Now(),
		Domain:    domain,
		Data: &st.StatsData{
			Data: &st.StatsData_Bounced{
				Bounced: &st.StatsDataBounced{
					Permanent: true,
					Code:      uint32(code),
					Msg:       errMsg,
				},
			},
		},
	}

	logrus.Infof("[🤷 got bounce] %vs - %d - %s", utils.ObfuscateEmail(email), code, errMsg)

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

func Run(ctx context.Context, config Config) {
	natsURL := viper.GetString("nats_url")
	addr := config.GetAddress()
	domain := config.GetDomain()
	readTimeout := config.GetReadTimeout()
	writeTimeout := config.GetWriteTimeout()
	maxPayload := config.GetMaxPayload()
	maxRecipients := config.GetMaxRecipients()

	nc, _, closeNats := utils.MustGetNats(natsURL)
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
	s.MaxMessageBytes = int64(maxPayload)
	s.MaxRecipients = int(maxRecipients)
	s.AllowInsecureAuth = true

	go func() {
		logrus.Printf("Starting server at: %v", s.Addr)
		if err := s.ListenAndServe(); err != nil {
			logrus.Fatalf("error serving: %v", err)
		}
	}()

	<-ctx.Done()
}

var parseMessageReg = regexp.MustCompile(`^Diagnostic-Code: (SMTP; ([0-9]+) .*$)`)

func parseCode(msg io.Reader) (int, string) {
	scanner := bufio.NewScanner(msg)
	for scanner.Scan() {
		line := scanner.Text()
		m := parseMessageReg.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		msg := m[1]
		code, err := strconv.Atoi(m[2])
		if err != nil {
			continue
		}
		return code, msg
	}

	return 550, "unknown error"
}
