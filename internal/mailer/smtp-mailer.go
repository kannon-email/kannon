package mailer

import (
	"bytes"
	"fmt"

	"github.com/google/uuid"
	log "github.com/sirupsen/logrus"
	"gopkg.in/mail.v2"
	"gorm.io/gorm"
	"smtp.ludusrusso.space/internal/db"
	"smtp.ludusrusso.space/internal/dkim"
	"smtp.ludusrusso.space/internal/smtp"
)

type headers map[string]string

type smtpMailer struct {
	Sender  smtp.Sender
	headers headers
	db      gorm.DB
}

type sendData struct {
	From    string
	Sender  string
	To      string
	Subject string
	Params  map[string]interface{}
}

func (m *smtpMailer) Send(email *db.SendingPoolEmail) error {
	err := m.sendEmail(email)
	if err != nil {
		email.Error = err.Error()
		email.Status = db.SendingPoolStatusError
	} else {
		email.Status = db.SendingPoolStatusSent
	}
	return m.db.Save(email).Error
}

func (m *smtpMailer) sendEmail(email *db.SendingPoolEmail) error {
	var pool db.SendingPool
	err := m.db.Find(&pool, email.SendingPoolID).Error
	if err != nil {
		return err
	}

	var domain db.Domain
	err = m.db.Find(&domain, "domain = ?", pool.Domain).Error
	if err != nil {
		return err
	}

	var template db.Template
	err = m.db.Find(&template, email.SendingPoolID).Error
	if err != nil {
		return err
	}
	data := sendData{
		From:    pool.Sender.Email,
		Sender:  pool.Sender.GetSender(),
		To:      email.To,
		Subject: pool.Subject,
	}

	msg, err := m.prepareMessage(data, template.HTML)
	if err != nil {
		return err
	}

	signData := dkim.SignData{
		PrivateKey: domain.DKIMKeys.PrivateKey,
		Domain:     domain.Domain,
		Selector:   "smtp",
		Headers:    []string{"From", "To", "Subject", "Message-ID"},
	}

	signedMsg, err := dkim.SignMessage(signData, bytes.NewReader(msg))
	if err != nil {
		return err
	}
	err = m.Sender.Send(data.From, data.To, signedMsg)
	if err != nil {
		return err
	}

	return nil
}

func (m *smtpMailer) prepareMessage(data sendData, html string) ([]byte, error) {
	headers := headers(m.headers)
	headers["Subject"] = data.Subject
	headers["From"] = data.Sender
	headers["To"] = data.To

	id, err := uuid.NewRandom()
	if err != nil {
		return nil, err
	}
	headers["Message-ID"] = fmt.Sprintf("<%v@%v>", id.String(), m.Sender.SenderName())

	return renderMsg(html, data.From, data.To, headers)
}

// NewSMTPMailer creates an SMTP mailer
func NewSMTPMailer(sender smtp.Sender) Mailer {
	return &smtpMailer{
		Sender: sender,
		headers: headers{
			"X-Sender": "Smtp Mailer",
		},
	}
}

// ToEmailMsg render a MsgPayload to an SMTP message
func renderMsg(html string, from, to string, headers headers) ([]byte, error) {
	msg := mail.NewMessage()

	for key, value := range headers {
		msg.SetHeader(key, value)
	}

	msg.SetBody("text/html", html)

	var buff bytes.Buffer
	if _, err := msg.WriteTo(&buff); err != nil {
		log.Warnf("🤢 Error writing message: %v\n", err)
		return nil, err
	}

	return buff.Bytes(), nil
}
