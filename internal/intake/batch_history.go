package intake

import (
	"context"
	"encoding/json"
	"github.com/kannon-email/kannon/internal/authz"
	"github.com/kannon-email/kannon/internal/batch"
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

// HistoryQuery is a bounded keyset read, independent of transport cursors.
type HistoryQuery struct {
	Limit     int
	AfterID   string
	AfterTime time.Time
	Status    string
}
type BatchReader interface {
	ReadBatch(context.Context, values.DomainName, string) (*BatchHistory, error)
	ListBatches(context.Context, values.DomainName, HistoryQuery) ([]BatchHistory, error)
	ListDeliveries(context.Context, values.DomainName, string, HistoryQuery) ([]DeliveryHistory, error)
}

func (s *InlineService) ReadBatch(ctx context.Context, d values.DomainName, id string) (*BatchHistory, error) {
	return authz.Guard(ctx, authz.Read, authz.Batches(d), func() (*BatchHistory, error) { return s.store.ReadBatch(ctx, d, id) })
}
func (s *InlineService) ListBatches(ctx context.Context, d values.DomainName, q HistoryQuery) ([]BatchHistory, error) {
	return authz.Guard(ctx, authz.Read, authz.Batches(d), func() ([]BatchHistory, error) { return s.store.ListBatches(ctx, d, q) })
}
func (s *InlineService) ListDeliveries(ctx context.Context, d values.DomainName, id string, q HistoryQuery) ([]DeliveryHistory, error) {
	return authz.Guard(ctx, authz.Read, authz.Batches(d), func() ([]DeliveryHistory, error) { return s.store.ListDeliveries(ctx, d, id, q) })
}
