package mailbuilder

import (
	"bytes"
	"context"
	"database/sql"

	"kannon.gyozatech.dev/generated/pb"
	"kannon.gyozatech.dev/generated/sqlc"
	"kannon.gyozatech.dev/internal/dkim"
	"kannon.gyozatech.dev/internal/pool"
)

type MailBulder interface {
	PerpareForSend(email sqlc.SendingPoolEmail) (pb.EmailToSend, error)
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

func (m *mailBuilder) PerpareForSend(email sqlc.SendingPoolEmail) (pb.EmailToSend, error) {
	emailData, err := m.db.GetSendingData(context.TODO(), email.MessageID)
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
	headers := buildHeaders(subject, sender, to, messageID, emailMessageID, baseHeaders)
	return renderMsg(html, sender.Email, to, headers)
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
