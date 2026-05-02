// Package delivery defines the Delivery domain entity per CONTEXT.md: the
// per-recipient transmission of a Batch. It encapsulates retry/backoff and
// scheduled-time semantics; the storage row is sqlc.SendingPoolEmail.
package delivery

import (
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/kannon-email/kannon/internal/batch"
)

// Domain errors.
var (
	ErrDeliveryNotFound = errors.New("delivery not found")
)

// Delivery is the per-recipient transmission unit of a Batch.
type Delivery struct {
	batchID               batch.ID
	email                 string
	fields                map[string]string
	sendAttempts          int
	domain                string
	scheduledTime         time.Time
	originalScheduledTime time.Time
}

// NewParams contains all fields needed to create a fresh Delivery.
type NewParams struct {
	BatchID       batch.ID
	Email         string
	Fields        map[string]string
	Domain        string
	ScheduledTime time.Time
}

// New creates a new Delivery scheduled for first attempt.
func New(p NewParams) (*Delivery, error) {
	if p.BatchID.IsZero() {
		return nil, fmt.Errorf("batch ID is required")
	}
	if p.Email == "" {
		return nil, fmt.Errorf("email is required")
	}
	if p.Domain == "" {
		return nil, fmt.Errorf("domain is required")
	}
	return &Delivery{
		batchID:               p.BatchID,
		email:                 p.Email,
		fields:                p.Fields,
		domain:                p.Domain,
		scheduledTime:         p.ScheduledTime,
		originalScheduledTime: p.ScheduledTime,
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

// NextRetryAt returns the time at which this Delivery should next be
// attempted, given its current attempt count and the original scheduled
// time. The repository uses this when applying a reschedule.
func (d *Delivery) NextRetryAt() time.Time {
	return d.originalScheduledTime.Add(rescheduleDelay(d.sendAttempts))
}

func rescheduleDelay(attempts int) time.Duration {
	delay := 2 * time.Minute * time.Duration(math.Pow(2, float64(attempts)))
	if delay < 5*time.Minute {
		delay = 5 * time.Minute
	}
	return delay
}
