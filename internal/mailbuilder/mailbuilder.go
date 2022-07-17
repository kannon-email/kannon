package mailbuilder

import (
	"bytes"
	"context"
	"strings"
	"time"

	"github.com/ludusrusso/kannon/generated/pb"
	sqlc "github.com/ludusrusso/kannon/internal/db"
	"github.com/ludusrusso/kannon/internal/dkim"
	"github.com/ludusrusso/kannon/internal/pool"
	"github.com/ludusrusso/kannon/internal/statssec"
	"github.com/sirupsen/logrus"
	"gopkg.in/mail.v2"
)

type MailBulder interface {
	PerpareForSend(ctx context.Context, email sqlc.SendingPoolEmail) (pb.EmailToSend, error)
}

// NewMailBuilder creates an SMTP mailer
func NewMailBuilder(q *sqlc.Queries, st statssec.StatsService) MailBulder {
	return &mailBuilder{
		db: q,
		st: st,
		headers: headers{
			"X-Mailer": "SMTP Mailer",
		},
	}
}

type mailBuilder struct {
	headers headers
	db      *sqlc.Queries
	st      statssec.StatsService
}

func (m *mailBuilder) PerpareForSend(ctx context.Context, email sqlc.SendingPoolEmail) (pb.EmailToSend, error) {
	emailData, err := m.db.GetSendingData(ctx, email.MessageID)
	if err != nil {
		return pb.EmailToSend{}, err
	}

	sender := pool.Sender{
		Email: emailData.SenderEmail,
		Alias: emailData.SenderAlias,
	}

	msg, err := m.prepareMessage(ctx, sender, emailData.Subject, email.Email, emailData.Domain, emailData.MessageID, emailData.Html, m.headers)
	if err != nil {
		return pb.EmailToSend{}, err
	}

	signedMsg, err := signMessage(emailData.Domain, emailData.DkimPrivateKey, msg)
	if err != nil {
		return pb.EmailToSend{}, err
	}

	return pb.EmailToSend{
		From:       emailData.SenderEmail,
		To:         email.Email,
		Body:       signedMsg,
		MessageId:  buildEmailMessageID(email.Email, emailData.MessageID),
		ReturnPath: buildReturnPath(email.Email, emailData.MessageID),
	}, nil
}

func (m *mailBuilder) prepareMessage(ctx context.Context, sender pool.Sender, subject string, to string, domain string, messageID string, html string, baseHeaders headers) ([]byte, error) {
	emailMessageID := buildEmailMessageID(to, messageID)
	html, err := m.preparedHtml(ctx, html, to, domain, emailMessageID)
	if err != nil {
		return nil, err
	}
	h := buildHeaders(subject, sender, to, messageID, emailMessageID, baseHeaders)
	return renderMsg(html, h)
}

func signMessage(domain string, dkimPrivateKey string, msg []byte) ([]byte, error) {
	signData := dkim.SignData{
		PrivateKey: dkimPrivateKey,
		Domain:     domain,
		Selector:   "kannon",
		Headers:    []string{"From", "To", "Subject", "Message-ID"},
	}

	return dkim.SignMessage(signData, bytes.NewReader(msg))
}

func (s *mailBuilder) preparedHtml(ctx context.Context, html string, email string, domain string, messageID string) (string, error) {
	link, err := s.buildTrackLink(ctx, email, messageID, domain)
	if err != nil {
		return "", err
	}
	html = insertTrackLinkInHtml(html, link)
	return html, nil
}

// renderMsg render a MsgPayload to an SMTP message
func renderMsg(html string, headers headers) ([]byte, error) {
	msg := mail.NewMessage()

	for key, value := range headers {
		msg.SetHeader(key, value)
	}
	msg.SetDateHeader("Date", time.Now())
	msg.SetBody("text/html", html)

	var buff bytes.Buffer
	if _, err := msg.WriteTo(&buff); err != nil {
		logrus.Warnf("🤢 Error writing message: %v\n", err)
		return nil, err
	}

	return buff.Bytes(), nil
}

func (m *mailBuilder) buildTrackLink(ctx context.Context, email string, messageID string, domain string) (string, error) {
	token, err := m.st.CreateOpenToken(ctx, messageID, email)
	if err != nil {
		return "", err
	}
	url := "https://stats." + domain + "/o/" + token
	return url, nil
}

func insertTrackLinkInHtml(html string, link string) string {
	return strings.Replace(html, "</body>", "<img src=\""+link+"\" style=\"display:none;\" /></body>", 1)
}
