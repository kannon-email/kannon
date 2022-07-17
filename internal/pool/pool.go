package pool

import (
	"context"
	"time"

	sqlc "github.com/ludusrusso/kannon/internal/db"
	"github.com/ludusrusso/kannon/internal/utils"
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
		MessageID:   utils.CreateMessageID(domain),
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
	msgID, _ := utils.ExtractMsgIDAndDomain(messageID)
	return m.db.SetSendingPoolError(ctx, sqlc.SetSendingPoolErrorParams{
		Email:     email,
		MessageID: msgID,
		ErrorMsg:  errMsg,
	})
}

func (m *sendingPoolManager) SetDelivered(ctx context.Context, messageID string, email string) error {
	msgID, _ := utils.ExtractMsgIDAndDomain(messageID)
	return m.db.SetSendingPoolDelivered(ctx, sqlc.SetSendingPoolDeliveredParams{
		Email:     email,
		MessageID: msgID,
	})
}

func NewSendingPoolManager(q *sqlc.Queries) SendingPoolManager {
	return &sendingPoolManager{
		db: q,
	}
}
