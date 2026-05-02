package pool

import (
	"context"
	"time"

	"github.com/kannon-email/kannon/internal/batch"
	"github.com/kannon-email/kannon/internal/delivery"
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
	// schedules a Delivery for each recipient.
	AddRecipientsPool(ctx context.Context, b *batch.Batch, recipients []*pb.Recipient, scheduled time.Time) error
	PrepareForSend(ctx context.Context, max uint) ([]*delivery.Delivery, error)
	PrepareForValidate(ctx context.Context, max uint) ([]*delivery.Delivery, error)
	SetScheduled(ctx context.Context, batchID batch.ID, email string) error
	RescheduleEmail(ctx context.Context, batchID batch.ID, email string) error
	CleanEmail(ctx context.Context, batchID batch.ID, email string) error
}

type sendingPoolManager struct {
	batches    batch.Repository
	deliveries delivery.Repository
}

func (m *sendingPoolManager) AddRecipientsPool(ctx context.Context, b *batch.Batch, recipients []*pb.Recipient, scheduled time.Time) error {
	if err := m.batches.Create(ctx, b); err != nil {
		return err
	}

	for _, r := range recipients {
		d, err := delivery.New(delivery.NewParams{
			BatchID:       b.ID(),
			Email:         r.Email,
			Fields:        r.Fields,
			Domain:        b.Domain(),
			ScheduledTime: scheduled,
		})
		if err != nil {
			return err
		}
		if err := m.deliveries.Schedule(ctx, d); err != nil {
			return err
		}
	}

	return nil
}

func (m *sendingPoolManager) PrepareForSend(ctx context.Context, max uint) ([]*delivery.Delivery, error) {
	return m.deliveries.PrepareForSend(ctx, int(max))
}

func (m *sendingPoolManager) PrepareForValidate(ctx context.Context, max uint) ([]*delivery.Delivery, error) {
	return m.deliveries.PrepareForValidate(ctx, int(max))
}

func (m *sendingPoolManager) CleanEmail(ctx context.Context, batchID batch.ID, email string) error {
	return m.deliveries.Clean(ctx, batchID, email)
}

func (m *sendingPoolManager) SetScheduled(ctx context.Context, batchID batch.ID, email string) error {
	return m.deliveries.SetScheduled(ctx, batchID, email)
}

func (m *sendingPoolManager) RescheduleEmail(ctx context.Context, batchID batch.ID, email string) error {
	return m.deliveries.Reschedule(ctx, batchID, email)
}

// NewSendingPoolManager wires a SendingPoolManager backed by the given Batch
// and Delivery repositories.
func NewSendingPoolManager(batches batch.Repository, deliveries delivery.Repository) SendingPoolManager {
	return &sendingPoolManager{
		batches:    batches,
		deliveries: deliveries,
	}
}
