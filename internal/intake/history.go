package intake

import (
	"context"
	"github.com/kannon-email/kannon/internal/apperr"
	"github.com/kannon-email/kannon/internal/authz"
	"github.com/kannon-email/kannon/internal/tracking"
	"github.com/kannon-email/kannon/internal/values"
	"time"
)

var (
	ErrDeliveryNotFound = apperr.New(apperr.NotFound, "not_found", "delivery not found")
	ErrBatchNotFound    = apperr.New(apperr.NotFound, "not_found", "batch not found")
)

// DeliveryHistory is the durable accepted Delivery, independent of the worker Pool.
type DeliveryHistory struct {
	ID, BatchID, Domain, Email        string
	Status                            DeliveryStatus
	CreatedAt, ScheduledAt, UpdatedAt time.Time
	AttemptCount                      int
	Tracking                          tracking.Policy
	ErrorCode, ErrorDetail            string
}

// GetDelivery authorizes a history read before querying accepted recipient data.
func (s *InlineService) GetDelivery(ctx context.Context, domain values.DomainName, batchID, id string) (*DeliveryHistory, error) {
	return authz.Guard(ctx, authz.Read, authz.Batches(domain), func() (*DeliveryHistory, error) {
		return s.store.GetDelivery(ctx, domain, batchID, id)
	})
}
