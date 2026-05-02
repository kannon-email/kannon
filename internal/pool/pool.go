package pool

import (
	"context"
	"math"
	"time"

	"github.com/kannon-email/kannon/internal/batch"
	sqlc "github.com/kannon-email/kannon/internal/db"
	pb "github.com/kannon-email/kannon/proto/kannon/mailer/types"
)

// Sender is the visible from-identity of a Batch. It is retained here for
// backwards compatibility with callers that still build the value type
// inline; new code should construct batch.Sender directly. This local
// alias is removed in a follow-up slice (parent PRD #322 §8).
type Sender = batch.Sender

// SendingPoolManager is a manager for sending pool
type SendingPoolManager interface {
	// AddRecipientsPool persists the Batch via the Batch repository and
	// schedules a per-recipient row in the sending pool for each recipient.
	AddRecipientsPool(ctx context.Context, b *batch.Batch, recipients []*pb.Recipient, scheduled time.Time) error
	PrepareForSend(ctx context.Context, max uint) ([]sqlc.SendingPoolEmail, error)
	PrepareForValidate(ctx context.Context, max uint) ([]sqlc.SendingPoolEmail, error)
	SetScheduled(ctx context.Context, messageID string, email string) error
	RescheduleEmail(ctx context.Context, messageID string, email string) error
	CleanEmail(ctx context.Context, messageID string, email string) error
}

type sendingPoolManager struct {
	db      *sqlc.Queries
	batches batch.Repository
}

// AddRecipientsPool persists the Batch and creates one sending pool row per recipient.
func (m *sendingPoolManager) AddRecipientsPool(ctx context.Context, b *batch.Batch, recipients []*pb.Recipient, scheduled time.Time) error {
	if err := m.batches.Create(ctx, b); err != nil {
		return err
	}

	for _, r := range recipients {
		err := m.db.CreatePool(ctx, sqlc.CreatePoolParams{
			MessageID:     b.ID().String(),
			Email:         r.Email,
			Fields:        r.Fields,
			ScheduledTime: sqlc.PgTimestampFromTime(scheduled),
			Domain:        b.Domain(),
		})
		if err != nil {
			return err
		}
	}

	return nil
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

func NewSendingPoolManager(q *sqlc.Queries, batches batch.Repository) SendingPoolManager {
	return &sendingPoolManager{
		db:      q,
		batches: batches,
	}
}

func computeRescheduleDelay(attempts int) time.Duration {
	rescheduleDelay := 2 * time.Minute * time.Duration(math.Pow(2, float64(attempts)))
	if rescheduleDelay < 5*time.Minute {
		rescheduleDelay = 5 * time.Minute
	}
	return rescheduleDelay
}
