package delivery

import (
	"context"
	"time"

	"github.com/kannon-email/kannon/internal/batch"
)

// InFlight names the operation a worker has claimed a Delivery for. There are
// exactly two, one per claiming worker, and the value alone determines both
// where a claim of that kind was taken from and whether handing the Delivery
// back counts as a spent send attempt — so a caller cannot pair the wrong
// status with the wrong bump.
type InFlight uint8

const (
	// InFlightForDispatch is a Delivery the Dispatcher has claimed for
	// transmission (see Repository.PrepareForSend).
	InFlightForDispatch InFlight = iota + 1

	// InFlightForValidation is a Delivery the Validator has claimed for
	// address validation (see Repository.PrepareForValidate).
	InFlightForValidation
)

// String names the claim for logs.
func (f InFlight) String() string {
	switch f {
	case InFlightForDispatch:
		return "dispatch"
	case InFlightForValidation:
		return "validation"
	default:
		return "unknown"
	}
}

// Repository persists Delivery entities (per-recipient sending pool rows).
type Repository interface {
	// Schedule persists one or more Delivery rows in the pool atomically:
	// all rows are inserted, or none are.
	Schedule(ctx context.Context, ds ...*Delivery) error

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

	// ReclaimStranded hands back to the pool up to max Deliveries that have
	// been claimed for f since longer ago than olderThan, and returns them as
	// they now stand — pre-claim state, no claim held, attempt counter bumped
	// if a claim of that kind spends one.
	//
	// olderThan is measured from the moment of the claim, never from the
	// scheduled time: a claim does not move the scheduled time, so under a
	// backlog a Delivery claimed one second ago looks hours overdue.
	//
	// It recovers; it never terminates. Only the Retry Budget ends a Delivery
	// (ADR 0007).
	ReclaimStranded(ctx context.Context, f InFlight, olderThan time.Duration, max int) ([]*Delivery, error)
}
