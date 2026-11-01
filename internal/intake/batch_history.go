package intake

import (
	"context"
	"github.com/kannon-email/kannon/internal/authz"
	"github.com/kannon-email/kannon/internal/batch"
	"github.com/kannon-email/kannon/internal/keyset"
	"github.com/kannon-email/kannon/internal/tracking"
	"github.com/kannon-email/kannon/internal/values"
	"time"
)

// HistoryRetention is how long a completed Batch and its Deliveries stay
// readable before cleanup removes them (ADR 0015).
const HistoryRetention = 30 * 24 * time.Hour

// HistoryCutoff is the earliest completion time still retained at now.
func HistoryCutoff(now time.Time) time.Time { return now.Add(-HistoryRetention) }

// ReceiptRetention is how long a completed Batch's idempotency receipt outlives
// it. Until then its key replays the Batch; afterwards the key is free again and
// reusing it creates a new Batch.
const ReceiptRetention = 90 * 24 * time.Hour

// ReceiptCutoff is the earliest completion time whose receipt is still retained at now.
func ReceiptCutoff(now time.Time) time.Time { return now.Add(-ReceiptRetention) }

// BatchStatus summarizes where a Batch is in its lifecycle.
type BatchStatus string

const (
	BatchPending    BatchStatus = "pending"
	BatchProcessing BatchStatus = "processing"
	BatchCompleted  BatchStatus = "completed"
)

// DeliveryStatus is the current state of one accepted Delivery.
type DeliveryStatus string

const (
	DeliveryPending   DeliveryStatus = "pending"
	DeliveryScheduled DeliveryStatus = "scheduled"
	DeliverySending   DeliveryStatus = "sending"
	DeliveryDelivered DeliveryStatus = "delivered"
	DeliveryRejected  DeliveryStatus = "rejected"
	DeliveryBounced   DeliveryStatus = "bounced"
	DeliveryFailed    DeliveryStatus = "failed"
)

// ParseDeliveryStatus accepts only the known Delivery states.
func ParseDeliveryStatus(s string) (DeliveryStatus, bool) {
	switch v := DeliveryStatus(s); v {
	case DeliveryPending, DeliveryScheduled, DeliverySending, DeliveryDelivered, DeliveryRejected, DeliveryBounced, DeliveryFailed:
		return v, true
	}
	return "", false
}

// Final reports whether the Delivery has reached an outcome.
func (s DeliveryStatus) Final() bool {
	switch s {
	case DeliveryDelivered, DeliveryRejected, DeliveryBounced, DeliveryFailed:
		return true
	}
	return false
}

// DeliveryCounts is the number of a Batch's Deliveries in each status.
type DeliveryCounts map[DeliveryStatus]int

// BatchHistory combines immutable acceptance data with current outcome counts.
type BatchHistory struct {
	SourceTemplateID             string
	ID, Domain, Subject, HTML    string
	Sender                       batch.Sender
	CreatedAt, ScheduledAt       time.Time
	CompletedAt                  *time.Time
	AcceptedCount, RejectedCount int
	Counts                       DeliveryCounts
	Tracking                     tracking.Policy
	Attachments                  []AttachmentMetadata
	Headers                      batch.Headers
	OneClickUnsubscribe          batch.OneClickUnsubscribe
	ReplyTo                      batch.ReplyTo
}

// Status is Completed once every Delivery has an outcome, Processing once any
// Delivery is sending or finished, and Pending before that.
func (b BatchHistory) Status() BatchStatus {
	if b.CompletedAt != nil {
		return BatchCompleted
	}
	for s, n := range b.Counts {
		if n > 0 && (s == DeliverySending || s.Final()) {
			return BatchProcessing
		}
	}
	return BatchPending
}

// ExpiresAt is when a completed Batch leaves history; nil while it is open.
func (b BatchHistory) ExpiresAt() *time.Time {
	if b.CompletedAt == nil {
		return nil
	}
	t := b.CompletedAt.Add(HistoryRetention)
	return &t
}

// BatchPosition is a Batch's place in the newest-first history order: creation time, then ID.
type BatchPosition struct {
	CreatedAt time.Time
	ID        string
}

// PositionOf reports where b sits in the newest-first history order.
func (b BatchHistory) PositionOf() BatchPosition {
	return BatchPosition{CreatedAt: b.CreatedAt, ID: b.ID}
}

// BatchReader lists Batches newest first and a Batch's Deliveries in ascending ID order. An empty
// status matches every Delivery.
type BatchReader interface {
	ReadBatch(context.Context, values.DomainName, string) (*BatchHistory, error)
	ListBatches(context.Context, values.DomainName, keyset.Page[BatchPosition]) ([]BatchHistory, error)
	ListDeliveries(ctx context.Context, d values.DomainName, batchID string, status DeliveryStatus, page keyset.Page[string]) ([]DeliveryHistory, error)
}

func (s *InlineService) ReadBatch(ctx context.Context, d values.DomainName, id string) (*BatchHistory, error) {
	return authz.Guard(ctx, authz.Read, authz.Batches(d), func() (*BatchHistory, error) { return s.store.ReadBatch(ctx, d, id) })
}
func (s *InlineService) ListBatches(ctx context.Context, d values.DomainName, page keyset.Page[BatchPosition]) ([]BatchHistory, error) {
	return authz.Guard(ctx, authz.Read, authz.Batches(d), func() ([]BatchHistory, error) { return s.store.ListBatches(ctx, d, page) })
}
func (s *InlineService) ListDeliveries(ctx context.Context, d values.DomainName, id string, status DeliveryStatus, page keyset.Page[string]) ([]DeliveryHistory, error) {
	return authz.Guard(ctx, authz.Read, authz.Batches(d), func() ([]DeliveryHistory, error) { return s.store.ListDeliveries(ctx, d, id, status, page) })
}
