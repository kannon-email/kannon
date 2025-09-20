package mailbuilder

import (
	"bytes"
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"

	sqlc "github.com/kannon-email/kannon/internal/db"
	"github.com/kannon-email/kannon/internal/dkim"
	"github.com/kannon-email/kannon/internal/pool"
	"github.com/kannon-email/kannon/internal/statssec"
	"github.com/kannon-email/kannon/internal/utils"
	pb "github.com/kannon-email/kannon/proto/kannon/mailer/types"
	"github.com/sirupsen/logrus"
	"gopkg.in/mail.v2"
)

const maxRetry = 10

type MailBulder interface {
	BuildEmail(ctx context.Context, email sqlc.SendingPoolEmail) (*pb.EmailToSend, error)
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

func (m *mailBuilder) BuildEmail(ctx context.Context, email sqlc.SendingPoolEmail) (*pb.EmailToSend, error) {
	emailData, err := m.db.GetSendingData(ctx, email.MessageID)
	if err != nil {
		return nil, err
	}

	sender := pool.Sender{
		Email: emailData.SenderEmail,
		Alias: emailData.SenderAlias,
	}

	attachments := make(Attachments)
	for name, r := range emailData.Attachments {
		attachments[name] = bytes.NewReader(r)
	}

	returnPath := buildReturnPath(email.Email, emailData.MessageID)
	msg, err := m.prepareMessage(ctx, sender, emailData.Subject, email.Email, emailData.Domain, emailData.MessageID, emailData.Html, m.headers, email.Fields, attachments)
	if err != nil {
		return nil, err
	}

	signedMsg, err := signMessage(emailData.Domain, emailData.DkimPrivateKey, msg)
	if err != nil {
		return nil, err
	}

	return &pb.EmailToSend{
		From:        emailData.SenderEmail,
		ReturnPath:  returnPath,
		To:          email.Email,
		Body:        signedMsg,
		EmailId:     buildEmailID(email.Email, emailData.MessageID),
		ShouldRetry: email.SendAttemptsCnt < maxRetry,
	}, nil
}

func (m *mailBuilder) prepareMessage(ctx context.Context, sender pool.Sender, subject string, to string, domain string, messageID string, html string, baseHeaders headers, fields map[string]string, attachments Attachments) ([]byte, error) {
	emailMessageID := buildEmailID(to, messageID)
	html, err := m.preparedHTML(ctx, html, to, domain, messageID, fields)
	if err != nil {
		return nil, err
	}
	subject = utils.ReplaceCustomFields(subject, fields)

	h := buildHeaders(subject, sender, to, messageID, emailMessageID, baseHeaders)
	return renderMsg(html, h, attachments)
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

func (m *mailBuilder) preparedHTML(ctx context.Context, html string, email string, domain string, messageID string, fields map[string]string) (string, error) {
	html = utils.ReplaceCustomFields(html, fields)
	html, err := m.replaceAllLinks(ctx, html, email, messageID, domain)
	if err != nil {
		return "", err
	}

	html, err = m.addTrackPixel(ctx, html, email, messageID, domain)
	if err != nil {
		return "", err
	}

	return html, nil
}

func (m *mailBuilder) replaceAllLinks(ctx context.Context, html string, email string, messageID string, domain string) (string, error) {
	return replaceLinks(html, func(link string) (string, error) {
		buildTrackClickLink, err := m.buildTrackClickLink(ctx, link, email, messageID, domain)
		if err != nil {
			return "", err
		}
		return buildTrackClickLink, nil
	})
}

func (m *mailBuilder) addTrackPixel(ctx context.Context, html string, email string, messageID string, domain string) (string, error) {
	link, err := m.buildTrackOpenLink(ctx, email, messageID, domain)
	if err != nil {
		return "", err
	}
	html = insertTrackLinkInHTML(html, link)
	return html, nil
}

// renderMsg render a MsgPayload to an SMTP message
func renderMsg(html string, headers headers, attachments Attachments) ([]byte, error) {
	msg := mail.NewMessage()

	for key, value := range headers {
		msg.SetHeader(key, value)
	}
	msg.SetDateHeader("Date", time.Now())
	msg.SetBody("text/html", html)
	for name, r := range attachments {
		msg.AttachReader(name, r)
	}

	var buff bytes.Buffer
	if _, err := msg.WriteTo(&buff); err != nil {
		logrus.Warnf("🤢 Error writing message: %v\n", err)
		return nil, err
	}

	return buff.Bytes(), nil
}

func (m *mailBuilder) buildTrackClickLink(ctx context.Context, url string, email string, messageID string, domain string) (string, error) {
	token, err := m.st.CreateLinkToken(ctx, messageID, email, url)
	if err != nil {
		return "", err
	}

	return fmt.Sprintf("https://stats.%v/c/%v", domain, token), nil
}

func (m *mailBuilder) buildTrackOpenLink(ctx context.Context, email string, messageID string, domain string) (string, error) {
	token, err := m.st.CreateOpenToken(ctx, messageID, email)
	if err != nil {
		return "", err
	}

	return fmt.Sprintf("https://stats.%v/o/%v", domain, token), nil
}

func insertTrackLinkInHTML(html string, link string) string {
	return strings.Replace(html, "</body>", fmt.Sprintf(`<img src="%s" style="display:none;"/></body>`, link), 1)
}

var regLink = regexp.MustCompile(`<a\s+(?:[^>]*?\s+)?href=["'](.+?)["']`)

func replaceLinks(html string, replace func(link string) (string, error)) (string, error) {
	matches := regLink.FindAllStringSubmatch(html, -1)
	for _, match := range matches {
		if len(match) != 2 {
			continue
		}
		link := match[1]
		newLink, err := replace(link)
		if err != nil {
			return "", err
		}
		// Replace the entire matched href attribute, not just the URL
		oldHref := match[0]
		newHref := strings.Replace(oldHref, link, newLink, 1)
		html = strings.Replace(html, oldHref, newHref, 1)
	}
	return html, nil
}
