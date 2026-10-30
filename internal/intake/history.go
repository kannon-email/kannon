package intake

import (
	"context"
	"errors"
	"github.com/kannon-email/kannon/internal/authz"
	"github.com/kannon-email/kannon/internal/tracking"
	"github.com/kannon-email/kannon/internal/values"
	"time"
)

var ErrDeliveryNotFound = errors.New("delivery not found")

// DeliveryHistory is the durable accepted Delivery, independent of the worker Pool.
type DeliveryHistory struct {
	ID, BatchID, Domain, Email, Status string
	CreatedAt, ScheduledAt, UpdatedAt  time.Time
	AttemptCount                       int
	Tracking                           tracking.Policy
	ErrorCode, ErrorDetail             string
}

// DeliveryReader scopes every lookup to both its Domain and Batch.
type DeliveryReader interface {
	GetDelivery(context.Context, values.DomainName, string, string) (*DeliveryHistory, error)
}

// GetDelivery authorizes a history read before querying accepted recipient data.
func (s *InlineService) GetDelivery(ctx context.Context, domain values.DomainName, batchID, id string) (*DeliveryHistory, error) {
	return authz.Guard(ctx, authz.Read, authz.Batches(domain), func() (*DeliveryHistory, error) {
		reader, ok := s.store.(DeliveryReader)
		if !ok {
			return nil, errors.New("delivery history unavailable")
		}
		return reader.GetDelivery(ctx, domain, batchID, id)
	})
}
