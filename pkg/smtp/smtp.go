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
	return &Session{}, nil
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

func (s *Session) Rcpt(to string) error {
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

	logrus.Infof("[🤷 got bounce] %vs - %d - %s", email, code, errMsg)

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
