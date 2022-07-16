package pool

import (
	"context"
	"fmt"
	"regexp"
	"time"

	sqlc "github.com/ludusrusso/kannon/internal/db"

	"gopkg.in/lucsky/cuid.v1"
)

type Sender struct {
	Alias string
	Email string
}

// SendingPoolManager is a manger for sending pool
type SendingPoolManager interface {
	AddPool(ctx context.Context, template sqlc.Template, to []string, from Sender, subject string, domain string) (sqlc.Message, error)
	PrepareForSend(ctx context.Context, max uint) ([]sqlc.SendingPoolEmail, error)
	SetError(ctx context.Context, messageID string, email string, errMsg string) error
	SetDelivered(ctx context.Context, messageID string, email string) error
}

type sendingPoolManager struct {
	db *sqlc.Queries
}

// AddPool starts a new schedule in the pool
func (m *sendingPoolManager) AddPool(ctx context.Context, template sqlc.Template, to []string, from Sender, subject string, domain string) (sqlc.Message, error) {
	msg, err := m.db.CreateMessage(ctx, sqlc.CreateMessageParams{
		TemplateID:  template.TemplateID,
		Domain:      domain,
		Subject:     subject,
		SenderEmail: from.Email,
		SenderAlias: from.Alias,
		MessageID:   createMessageID(domain),
	})
	if err != nil {
		return sqlc.Message{}, err
	}

	_, err = m.db.CreatePool(ctx, sqlc.CreatePoolParams{
		ScheduledTime: time.Now(), // TODO
		MessageID:     msg.MessageID,
		Emails:        to,
	})
	if err != nil {
		return sqlc.Message{}, err
	}
	return msg, nil
}

func (m *sendingPoolManager) PrepareForSend(ctx context.Context, max uint) ([]sqlc.SendingPoolEmail, error) {
	return m.db.PrepareForSend(ctx, int32(max))
}

func (m *sendingPoolManager) SetError(ctx context.Context, messageID string, email string, errMsg string) error {
	msgId := extractMsgId(messageID)
	return m.db.SetSendingPoolError(ctx, sqlc.SetSendingPoolErrorParams{
		Email:     email,
		MessageID: msgId,
		ErrorMsg:  errMsg,
	})
}

func (m *sendingPoolManager) SetDelivered(ctx context.Context, messageID string, email string) error {
	msgId := extractMsgId(messageID)
	return m.db.SetSendingPoolDelivered(ctx, sqlc.SetSendingPoolDeliveredParams{
		Email:     email,
		MessageID: msgId,
	})
}

func NewSendingPoolManager(q *sqlc.Queries) (SendingPoolManager, error) {
	return &sendingPoolManager{
		db: q,
	}, nil
}

func createMessageID(domain string) string {
	return fmt.Sprintf("msg_%v@%v", cuid.New(), domain)
}

var extractMsgIDReg = regexp.MustCompile(`<.+\/(?P<messageId>.+)>`)

func extractMsgId(messageID string) string {
	match := extractMsgIDReg.FindStringSubmatch(messageID)
	return match[1]
}
