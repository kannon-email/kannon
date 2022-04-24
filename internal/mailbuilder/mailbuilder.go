package mailbuilder

import (
	"bytes"
	"context"
	"database/sql"
	"time"

	"github.com/sirupsen/logrus"
	"gopkg.in/mail.v2"
	"kannon.gyozatech.dev/generated/pb"
	"kannon.gyozatech.dev/generated/sqlc"
	"kannon.gyozatech.dev/internal/dkim"
	"kannon.gyozatech.dev/internal/pool"
)

type MailBulder interface {
	PerpareForSend(ctx context.Context, email sqlc.SendingPoolEmail) (pb.EmailToSend, error)
}

// NewMailBuilder creates an SMTP mailer
func NewMailBuilder(db *sql.DB) MailBulder {
	return &mailBuilder{
		db: sqlc.New(db),
		headers: headers{
			"X-Mailer": "SMTP Mailer",
		},
	}
}

type mailBuilder struct {
	headers headers
	db      *sqlc.Queries
}

func (m *mailBuilder) PerpareForSend(ctx context.Context, email sqlc.SendingPoolEmail) (pb.EmailToSend, error) {
	emailData, err := m.db.GetSendingData(ctx, email.MessageID)
	if err != nil {
		return pb.EmailToSend{}, err
	}

	msg, err := prepareMessage(pool.Sender{
		Email: emailData.SenderEmail,
		Alias: emailData.SenderAlias,
	}, emailData.Subject, email.Email, emailData.MessageID, emailData.Html, m.headers)
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

func prepareMessage(sender pool.Sender, subject string, to string, messageID string, html string, baseHeaders headers) ([]byte, error) {
	emailMessageID := buildEmailMessageID(to, messageID)
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
