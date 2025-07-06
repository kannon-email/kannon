package pool

import (
	"context"
	"math"
	"time"

	sqlc "github.com/kannon-email/kannon/internal/db"
	"github.com/kannon-email/kannon/internal/utils"
	pb "github.com/kannon-email/kannon/proto/kannon/mailer/types"
)

type Sender struct {
	Alias string
	Email string
}

// SendingPoolManager is a manger for sending pool
type SendingPoolManager interface {
	AddRecipientsPool(ctx context.Context, template sqlc.Template, recipents []*pb.Recipient, from Sender, scheduled time.Time, subject string, domain string, attachments sqlc.Attachments) (sqlc.Message, error)
	PrepareForSend(ctx context.Context, max uint) ([]sqlc.SendingPoolEmail, error)
	PrepareForValidate(ctx context.Context, max uint) ([]sqlc.SendingPoolEmail, error)
	SetScheduled(ctx context.Context, messageID string, email string) error
	RescheduleEmail(ctx context.Context, messageID string, email string) error
	CleanEmail(ctx context.Context, messageID string, email string) error
}

type sendingPoolManager struct {
	db *sqlc.Queries
}

// AddPool starts a new schedule in the pool
func (m *sendingPoolManager) AddRecipientsPool(ctx context.Context, template sqlc.Template, recipents []*pb.Recipient, from Sender, scheduled time.Time, subject string, domain string, attachments sqlc.Attachments) (sqlc.Message, error) {
	msg, err := m.db.CreateMessage(ctx, sqlc.CreateMessageParams{
		TemplateID:  template.TemplateID,
		Domain:      domain,
		Subject:     subject,
		SenderEmail: from.Email,
		SenderAlias: from.Alias,
		MessageID:   utils.CreateMessageID(domain),
		Attachments: attachments,
	})
	if err != nil {
		return sqlc.Message{}, err
	}

	for _, r := range recipents {
		err = m.db.CreatePool(ctx, sqlc.CreatePoolParams{
			MessageID:     msg.MessageID,
			Email:         r.Email,
			Fields:        r.Fields,
			ScheduledTime: sqlc.PgTimestampFromTime(scheduled),
			Domain:        domain,
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

func (m *sendingPoolManager) PrepareForValidate(ctx context.Context, max uint) ([]sqlc.SendingPoolEmail, error) {
	return m.db.PrepareForValidate(ctx, int32(max))
}

func (m *sendingPoolManager) CleanEmail(ctx context.Context, messageID string, email string) error {
	return m.db.CleanPool(ctx, sqlc.CleanPoolParams{
		Email:     email,
		MessageID: messageID,
	})
}

func (m *sendingPoolManager) SetScheduled(ctx context.Context, messageID string, email string) error {
	return m.db.SetSendingPoolScheduled(ctx, sqlc.SetSendingPoolScheduledParams{
		Email:     email,
		MessageID: messageID,
	})
}

func (m *sendingPoolManager) RescheduleEmail(ctx context.Context, messageID string, email string) error {
	pool, err := m.db.GetPool(ctx, sqlc.GetPoolParams{
		Email:     email,
		MessageID: messageID,
	})
	if err != nil {
		return err
	}

	rescheduleDelay := computeRescheduleDelay(int(pool.SendAttemptsCnt))

	scheduledTime := pool.OriginalScheduledTime.Time.Add(rescheduleDelay)

	return m.db.ReschedulePool(ctx, sqlc.ReschedulePoolParams{
		Email:         email,
		MessageID:     messageID,
		ScheduledTime: sqlc.PgTimestampFromTime(scheduledTime),
	})
}

func NewSendingPoolManager(q *sqlc.Queries) SendingPoolManager {
	return &sendingPoolManager{
		db: q,
	}
}

func computeRescheduleDelay(attempts int) time.Duration {
	rescheduleDelay := 2 * time.Minute * time.Duration(math.Pow(2, float64(attempts)))
	if rescheduleDelay < 5*time.Minute {
		rescheduleDelay = 5 * time.Minute
	}
	return rescheduleDelay
}
