package mailbuilder

import (
	"bytes"

	"gorm.io/gorm"
	"kannon.gyozatech.dev/generated/proto"
	"kannon.gyozatech.dev/internal/db"
	"kannon.gyozatech.dev/internal/dkim"
)

type MailBulder interface {
	PerpareForSend(email db.SendingPoolEmail) (proto.EmailToSend, error)
}

// NewMailBuilder creates an SMTP mailer
func NewMailBuilder(db *gorm.DB) MailBulder {
	return &mailBuilder{
		db: db,
		headers: headers{
			"X-Mailer": "SMTP Mailer",
		},
	}
}

type mailBuilder struct {
	headers headers
	db      *gorm.DB
}

func (m *mailBuilder) PerpareForSend(email db.SendingPoolEmail) (proto.EmailToSend, error) {
	pool := db.SendingPool{
		ID: email.SendingPoolID,
	}

	err := m.db.Where(&pool).First(&pool).Error
	if err != nil {
		return proto.EmailToSend{}, err
	}

	var domain db.Domain
	err = m.db.Find(&domain, "domain = ?", pool.Domain).Error
	if err != nil {
		return proto.EmailToSend{}, err
	}

	var template db.Template
	err = m.db.Find(&template, "template_id = ?", pool.TemplateID).Error
	if err != nil {
		return proto.EmailToSend{}, err
	}

	msg, err := prepareMessage(pool.Sender, pool.Subject, email.To, pool.MessageID, template.HTML, m.headers)
	if err != nil {
		return proto.EmailToSend{}, err
	}

	signedMsg, err := signMessage(domain, msg)
	if err != nil {
		return proto.EmailToSend{}, err
	}

	return proto.EmailToSend{
		From:       pool.Sender.Email,
		To:         email.To,
		Body:       signedMsg,
		MessageId:  buildEmailMessageID(email.To, pool.MessageID),
		ReturnPath: buildReturnPath(email.To, pool.MessageID),
	}, nil
}

func prepareMessage(sender db.Sender, subject string, to string, messageID string, html string, baseHeaders headers) ([]byte, error) {
	emailMessageID := buildEmailMessageID(to, messageID)
	headers := buildHeaders(subject, sender, to, messageID, emailMessageID, baseHeaders)
	return renderMsg(html, sender.Email, to, headers)
}

func signMessage(domain db.Domain, msg []byte) ([]byte, error) {
	signData := dkim.SignData{
		PrivateKey: domain.DKIMKeys.PrivateKey,
		Domain:     domain.Domain,
		Selector:   "smtp",
		Headers:    []string{"From", "To", "Subject", "Message-ID"},
	}

	return dkim.SignMessage(signData, bytes.NewReader(msg))
}
