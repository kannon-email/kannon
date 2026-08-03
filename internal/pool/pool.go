// Package pool exposes the claim/scheduling primitive that operates on
// delivery.Delivery values. The Claimer hides the enum-flip claim
// mechanism, the scheduled-time filter, and the exponential backoff
// behind a small interface (parent PRD #322 §8).
package pool

import (
	"context"
	"time"

	"github.com/kannon-email/kannon/internal/batch"
	"github.com/kannon-email/kannon/internal/delivery"
)

// Claimer atomically claims Deliveries from the sending pool and
// transitions them between in-flight states. Implementations are
// backed by the delivery.Repository and operate exclusively on
// delivery.Delivery values.
type Claimer interface {
	// ClaimForValidation atomically claims up to max deliveries that
	// are pending validation and returns them.
	ClaimForValidation(ctx context.Context, max int) ([]*delivery.Delivery, error)

	// ClaimForDispatch atomically claims up to max deliveries that are
	// scheduled and due (scheduled_time <= NOW()) for dispatch.
	ClaimForDispatch(ctx context.Context, max int) ([]*delivery.Delivery, error)

	// MarkValidated transitions a Delivery from to-validate to
	// scheduled, making it eligible for ClaimForDispatch.
	MarkValidated(ctx context.Context, d *delivery.Delivery) error

	// Reschedule applies the Delivery's retry policy: bumps the
	// attempt counter and rolls the scheduled time forward by the
	// exponential backoff window.
	Reschedule(ctx context.Context, d *delivery.Delivery) error

	// Drop removes a terminated Delivery from the pool.
	Drop(ctx context.Context, d *delivery.Delivery) error

	// Lookup loads a Delivery by its (BatchID, Email) key. Used by
	// stats-driven consumers that only have the storage key on hand.
	Lookup(ctx context.Context, batchID batch.ID, email string) (*delivery.Delivery, error)

	// ReclaimStranded hands back to the pool up to max Deliveries that have
	// been in flight for f since longer ago than olderThan — a worker died
	// holding the claim, or the outcome of the work never came back — and
	// returns them as they now stand, for the caller to log and count.
	//
	// Called by the worker that owns the claim: the Dispatcher reclaims what it
	// claimed for dispatch, the Validator what it claimed for validation. It
	// recovers and makes no claim about what happened to the Delivery in the
	// meantime; only the Retry Budget terminates one (ADR 0007).
	ReclaimStranded(ctx context.Context, f delivery.InFlight, olderThan time.Duration, max int) ([]*delivery.Delivery, error)
}

type claimer struct {
	deliveries delivery.Repository
}

// NewClaimer wires a Claimer backed by the given Delivery repository.
func NewClaimer(deliveries delivery.Repository) Claimer {
	return &claimer{deliveries: deliveries}
}

func (c *claimer) ClaimForValidation(ctx context.Context, max int) ([]*delivery.Delivery, error) {
	return c.deliveries.PrepareForValidate(ctx, max)
}

func (c *claimer) ClaimForDispatch(ctx context.Context, max int) ([]*delivery.Delivery, error) {
	return c.deliveries.PrepareForSend(ctx, max)
}

func (c *claimer) MarkValidated(ctx context.Context, d *delivery.Delivery) error {
	return c.deliveries.SetScheduled(ctx, d.BatchID(), d.Email())
}

func (c *claimer) Reschedule(ctx context.Context, d *delivery.Delivery) error {
	return c.deliveries.Reschedule(ctx, d.BatchID(), d.Email())
}

func (c *claimer) Drop(ctx context.Context, d *delivery.Delivery) error {
	return c.deliveries.Clean(ctx, d.BatchID(), d.Email())
}

func (c *claimer) Lookup(ctx context.Context, batchID batch.ID, email string) (*delivery.Delivery, error) {
	return c.deliveries.Get(ctx, batchID, email)
}

func (c *claimer) ReclaimStranded(ctx context.Context, f delivery.InFlight, olderThan time.Duration, max int) ([]*delivery.Delivery, error) {
	return c.deliveries.ReclaimStranded(ctx, f, olderThan, max)
}
