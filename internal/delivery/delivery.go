// Package delivery defines the Delivery domain entity per CONTEXT.md: the
// per-recipient transmission of a Batch. It encapsulates retry/backoff and
// scheduled-time semantics; the storage row is sqlc.SendingPoolEmail.
package delivery

import (
	"errors"
	"time"

	"github.com/kannon-email/kannon/internal/batch"
	"github.com/kannon-email/kannon/internal/tracking"
)

// Domain errors.
var (
	ErrDeliveryNotFound = errors.New("delivery not found")
)

// DefaultRetryWindow is the Retry Budget a Delivery is given when its caller
// states none: 24 hours from the moment its Batch asked for it to be sent
// (CONTEXT.md, *Retry Budget*). It reproduces the reach of the retry cap it
// replaced and coincides with the MaxAge of the kannon-sending stream, past
// which an Envelope cannot survive anyway (ADR 0007).
const DefaultRetryWindow = 24 * time.Hour

// Delivery is the per-recipient transmission unit of a Batch.
type Delivery struct {
	batchID               batch.ID
	email                 string
	fields                map[string]string
	sendAttempts          int
	domain                string
	scheduledTime         time.Time
	originalScheduledTime time.Time
	backoff               BackoffPolicy
	retryWindow           time.Duration
	tracking              tracking.Policy
}

// NewParams contains all fields needed to create a fresh Delivery.
type NewParams struct {
	BatchID       batch.ID
	Email         string
	Fields        map[string]string
	Domain        string
	ScheduledTime time.Time
	Backoff       BackoffPolicy
	// RetryWindow is this Delivery's Retry Budget. Zero substitutes
	// DefaultRetryWindow, on the same terms as Backoff.
	RetryWindow time.Duration
	// Tracking is the effective Tracking Policy for this Delivery, already
	// resolved by the caller at intake (ADR 0003).
	Tracking tracking.Policy
}

// New creates a new Delivery scheduled for first attempt. The Tracking Policy
// is normalised, so a fresh Delivery — and therefore the Pool row it becomes —
// always carries a concrete Policy, never one that states nothing.
func New(p NewParams) (*Delivery, error) {
	if p.BatchID.IsZero() {
		return nil, errors.New("batch ID is required")
	}
	if p.Email == "" {
		return nil, errors.New("email is required")
	}
	if p.Domain == "" {
		return nil, errors.New("domain is required")
	}
	return &Delivery{
		batchID:               p.BatchID,
		email:                 p.Email,
		fields:                p.Fields,
		domain:                p.Domain,
		scheduledTime:         p.ScheduledTime,
		originalScheduledTime: p.ScheduledTime,
		backoff:               policyOrDefault(p.Backoff),
		retryWindow:           windowOrDefault(p.RetryWindow),
		tracking:              p.Tracking.Normalized(),
	}, nil
}

// LoadParams contains all fields needed to rehydrate a Delivery from storage.
type LoadParams struct {
	BatchID               batch.ID
	Email                 string
	Fields                map[string]string
	SendAttempts          int
	Domain                string
	ScheduledTime         time.Time
	OriginalScheduledTime time.Time
	Backoff               BackoffPolicy
	RetryWindow           time.Duration
	Tracking              tracking.Policy
}

// Load rehydrates a Delivery from stored data (used by repository implementations).
func Load(p LoadParams) *Delivery {
	return &Delivery{
		batchID:               p.BatchID,
		email:                 p.Email,
		fields:                p.Fields,
		sendAttempts:          p.SendAttempts,
		domain:                p.Domain,
		scheduledTime:         p.ScheduledTime,
		originalScheduledTime: p.OriginalScheduledTime,
		backoff:               policyOrDefault(p.Backoff),
		retryWindow:           windowOrDefault(p.RetryWindow),
		tracking:              p.Tracking,
	}
}

// Getters

func (d *Delivery) BatchID() batch.ID         { return d.batchID }
func (d *Delivery) Email() string             { return d.email }
func (d *Delivery) Fields() map[string]string { return d.fields }
func (d *Delivery) SendAttempts() int         { return d.sendAttempts }
func (d *Delivery) Domain() string            { return d.domain }
func (d *Delivery) ScheduledTime() time.Time  { return d.scheduledTime }
func (d *Delivery) OriginalScheduledTime() time.Time {
	return d.originalScheduledTime
}

// TrackingPolicy is the Tracking Policy that governs this Delivery. It is
// carried state: the Domain/Batch/Recipient cascade was resolved when the Batch
// was created and frozen here, so the Delivery records the Policy that actually
// applied to it (ADR 0003). Nothing downstream resolves it again.
func (d *Delivery) TrackingPolicy() tracking.Policy { return d.tracking }

// NextRetryAt returns the time at which this Delivery should next be
// attempted, given its current attempt count and the original scheduled
// time. The repository uses this when applying a reschedule.
func (d *Delivery) NextRetryAt() time.Time {
	return d.originalScheduledTime.Add(d.backoff.Delay(d.sendAttempts))
}

// CanRetry reports whether this Delivery may be attempted again: the next
// retry must still fall inside the Retry Budget window.
//
// This is the one predicate that stops a Delivery being tried, consulted by
// every path that hands one back to the Pool (ADR 0007). It replaces the
// maxRetry = 10 constant that used to live in internal/envelope, and admits
// exactly the same retries: under DefaultBackoff the tenth retry falls at
// 2m·2⁹ = 17h04m, inside the 24h window, and the eleventh at 2m·2¹⁰ = 34h08m,
// outside it.
//
// It takes no clock. Both instants derive from originalScheduledTime, so the
// predicate reduces to backoff.Delay(sendAttempts) < retryWindow — deterministic
// in tests, and indifferent to how late the Delivery is being handled. That is
// the non-obvious virtue of measuring the budget from the Batch's own scheduled
// time: a Dispatcher that was down for three days does not mass-terminate the
// Batch it finds waiting on resumption, and a Delivery always gets at least one
// attempt however late it is offered one — by construction, not by a special
// case for the first attempt.
func (d *Delivery) CanRetry() bool {
	return d.NextRetryAt().Before(d.originalScheduledTime.Add(d.retryWindow))
}

func policyOrDefault(p BackoffPolicy) BackoffPolicy {
	if p == nil {
		return DefaultBackoff
	}
	return p
}

func windowOrDefault(w time.Duration) time.Duration {
	if w <= 0 {
		return DefaultRetryWindow
	}
	return w
}
