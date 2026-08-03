package smtp

import (
	"bufio"
	"fmt"
	"io"
	"log/slog"
	"net/mail"
	"regexp"
	"strconv"

	"github.com/emersion/go-smtp"
	"github.com/kannon-email/kannon/internal/publisher"
	"github.com/kannon-email/kannon/internal/utils"
	st "github.com/kannon-email/kannon/proto/kannon/stats/types"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// The Backend implements SMTP server methods.
//
// The bounce feed is held as a publisher.Publisher rather than a *nats.Conn
// (which satisfies it) so the subject a DSN lands on is observable in a test —
// see #376, where it silently drifted onto one nobody consumed.
type Backend struct {
	nc publisher.Publisher
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
	nc   publisher.Publisher
}

func (s *Session) AuthPlain(username, password string) error {
	return nil
}

func (s *Session) Mail(from string, opts *smtp.MailOptions) error {
	slog.Debug("Mail from: " + from)
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
		slog.Warn(fmt.Sprintf("Error parsing bounce return path: %s", err))
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
					Permanent: isPermanentCode(code),
					Code:      uint32(code),
					Msg:       errMsg,
				},
			},
		},
	}

	slog.Info(fmt.Sprintf("[🤷 got bounce] %vs - %d - %s", utils.ObfuscateEmail(email), code, errMsg))

	// PublishStat derives the subject from the payload, so an asynchronous
	// bounce lands on the same kannon.stats.bounced as a synchronous one.
	// Naming the subject by hand here is what put these events on a topic no
	// consumer subscribed to (#376).
	if err := publisher.PublishStat(s.nc, m); err != nil {
		slog.Error("Cannot publish data", "err", err)
		return nil
	}

	return nil
}

// isPermanentCode classifies a DSN diagnostic code by its SMTP reply class,
// the same rule the synchronous path applies to a live rejection
// (internal/smtp.newSMTPErrorFromSTMP). The Delivery is terminal either way —
// the flag records whether the address itself is worth writing off (5xx) or
// whether someone merely gave up after retrying (4xx: us on the synchronous
// path, the remote MTA on this one).
func isPermanentCode(code int) bool {
	return code >= 500 && code < 600
}

func (s *Session) Reset() {}

func (s *Session) Logout() error {
	return nil
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
