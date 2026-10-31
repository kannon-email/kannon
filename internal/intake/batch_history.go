package intake

import (
	"context"
	"encoding/json"
	"github.com/kannon-email/kannon/internal/authz"
	"github.com/kannon-email/kannon/internal/batch"
	"github.com/kannon-email/kannon/internal/keyset"
	"github.com/kannon-email/kannon/internal/tracking"
	"github.com/kannon-email/kannon/internal/values"
	"time"
)

// BatchHistory combines immutable acceptance data with current outcome counts.
type BatchHistory struct {
	SourceTemplateID             string
	ID, Domain, Subject, HTML    string
	Sender                       batch.Sender
	CreatedAt, ScheduledAt       time.Time
	CompletedAt                  *time.Time
	AcceptedCount, RejectedCount int
	Counts                       map[string]int
	Tracking                     tracking.Policy
	Attachments                  []AttachmentMetadata
	Headers                      json.RawMessage
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
	ListDeliveries(ctx context.Context, d values.DomainName, batchID, status string, page keyset.Page[string]) ([]DeliveryHistory, error)
}

func (s *InlineService) ReadBatch(ctx context.Context, d values.DomainName, id string) (*BatchHistory, error) {
	return authz.Guard(ctx, authz.Read, authz.Batches(d), func() (*BatchHistory, error) { return s.store.ReadBatch(ctx, d, id) })
}
func (s *InlineService) ListBatches(ctx context.Context, d values.DomainName, page keyset.Page[BatchPosition]) ([]BatchHistory, error) {
	return authz.Guard(ctx, authz.Read, authz.Batches(d), func() ([]BatchHistory, error) { return s.store.ListBatches(ctx, d, page) })
}
func (s *InlineService) ListDeliveries(ctx context.Context, d values.DomainName, id, status string, page keyset.Page[string]) ([]DeliveryHistory, error) {
	return authz.Guard(ctx, authz.Read, authz.Batches(d), func() ([]DeliveryHistory, error) { return s.store.ListDeliveries(ctx, d, id, status, page) })
}
