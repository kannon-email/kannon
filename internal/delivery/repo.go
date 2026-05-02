package delivery

import (
	"context"

	"github.com/kannon-email/kannon/internal/batch"
)

// Repository persists Delivery entities (per-recipient sending pool rows).
type Repository interface {
	// Schedule persists a new Delivery row in the pool.
	Schedule(ctx context.Context, d *Delivery) error

	// PrepareForSend atomically claims up to max scheduled deliveries for
	// dispatch and returns them.
	PrepareForSend(ctx context.Context, max int) ([]*Delivery, error)

	// PrepareForValidate atomically claims up to max to-validate deliveries
	// and returns them.
	PrepareForValidate(ctx context.Context, max int) ([]*Delivery, error)

	// Get loads a Delivery by its (BatchID, Email) key.
	// Returns ErrDeliveryNotFound if the row does not exist.
	Get(ctx context.Context, batchID batch.ID, email string) (*Delivery, error)

	// SetScheduled marks a Delivery as scheduled (validated, ready to send).
	SetScheduled(ctx context.Context, batchID batch.ID, email string) error

	// Reschedule applies the Delivery's retry policy: bumps the attempt
	// counter and rolls the scheduled time forward by NextRetryAt.
	Reschedule(ctx context.Context, batchID batch.ID, email string) error

	// Clean removes a terminated Delivery.
	Clean(ctx context.Context, batchID batch.ID, email string) error
}
