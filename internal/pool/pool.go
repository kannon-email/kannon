package pool

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/ludusrusso/kannon/generated/pb"
	sqlc "github.com/ludusrusso/kannon/internal/db"
	"github.com/ludusrusso/kannon/internal/utils"
)

type Sender struct {
	Alias string
	Email string
}

// SendingPoolManager is a manger for sending pool
type SendingPoolManager interface {
	AddPool(ctx context.Context, template sqlc.Template, to []string, from Sender, scheduled time.Time, subject string, domain string) (sqlc.Message, error)
	AddRecipientsPool(ctx context.Context, template sqlc.Template, recipents []*pb.Recipient, from Sender, scheduled time.Time, subject string, domain string) (sqlc.Message, error)
	PrepareForSend(ctx context.Context, max uint) ([]sqlc.SendingPoolEmail, error)
	SetError(ctx context.Context, messageID string, email string, errMsg string) error
	SetDelivered(ctx context.Context, messageID string, email string) error
}

type sendingPoolManager struct {
	db *sqlc.Queries
}

// AddPool starts a new schedule in the pool
func (m *sendingPoolManager) AddPool(ctx context.Context, template sqlc.Template, to []string, from Sender, scheduled time.Time, subject string, domain string) (sqlc.Message, error) {
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

	err = m.db.CreatePool(ctx, sqlc.CreatePoolParams{
		ScheduledTime: scheduled,
		MessageID:     msg.MessageID,
		Emails:        to,
	})
	if err != nil {
		return sqlc.Message{}, err
	}
	return msg, nil
}

// AddPool starts a new schedule in the pool
func (m *sendingPoolManager) AddRecipientsPool(ctx context.Context, template sqlc.Template, recipents []*pb.Recipient, from Sender, scheduled time.Time, subject string, domain string) (sqlc.Message, error) {
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

	for _, r := range recipents {
		fraw, err := json.Marshal(r.Fields)
		if err != nil {
			return sqlc.Message{}, fmt.Errorf("error marshaling fields: %w", err)
		}

		err = m.db.CreatePoolWithFields(ctx, sqlc.CreatePoolWithFieldsParams{
			MessageID: msg.MessageID,
			Email:     r.Email,
			Fields:    json.RawMessage(fraw),
		})
		if err != nil {
			return sqlc.Message{}, err
		}
	}

	return msg, nil
}

func (m *sendingPoolManager) PrepareForSend(ctx context.Context, max uint) ([]sqlc.SendingPoolEmail, error) {
	return m.db.PrepareForSend(ctx, int32(max))
}

func (m *sendingPoolManager) SetError(ctx context.Context, messageID string, email string, errMsg string) error {
	msgID, _, err := utils.ExtractMsgIDAndDomain(messageID)
	if err != nil {
		return err
	}
	return m.db.SetSendingPoolError(ctx, sqlc.SetSendingPoolErrorParams{
		Email:     email,
		MessageID: msgID,
		ErrorMsg:  errMsg,
	})
}

func (m *sendingPoolManager) SetDelivered(ctx context.Context, messageID string, email string) error {
	msgID, _, err := utils.ExtractMsgIDAndDomain(messageID)
	if err != nil {
		return err
	}
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
