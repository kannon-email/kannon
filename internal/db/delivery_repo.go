package sqlc

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"

	"github.com/kannon-email/kannon/internal/batch"
	"github.com/kannon-email/kannon/internal/delivery"
)

type deliveryRepository struct {
	q *Queries
}

// NewDeliveryRepository creates a new PostgreSQL-backed Delivery repository.
// It writes to and reads from the sending_pool_emails table.
func NewDeliveryRepository(q *Queries) delivery.Repository {
	return &deliveryRepository{q: q}
}

func (r *deliveryRepository) Schedule(ctx context.Context, d *delivery.Delivery) error {
	return r.q.CreatePool(ctx, CreatePoolParams{
		MessageID:     d.BatchID().String(),
		Email:         d.Email(),
		Fields:        toCustomFields(d.Fields()),
		ScheduledTime: PgTimestampFromTime(d.ScheduledTime()),
		Domain:        d.Domain(),
	})
}

func (r *deliveryRepository) PrepareForSend(ctx context.Context, max int) ([]*delivery.Delivery, error) {
	rows, err := r.q.PrepareForSend(ctx, int32(max))
	if err != nil {
		return nil, err
	}
	return rowsToDeliveries(rows), nil
}

func (r *deliveryRepository) PrepareForValidate(ctx context.Context, max int) ([]*delivery.Delivery, error) {
	rows, err := r.q.PrepareForValidate(ctx, int32(max))
	if err != nil {
		return nil, err
	}
	return rowsToDeliveries(rows), nil
}

func (r *deliveryRepository) Get(ctx context.Context, batchID batch.ID, email string) (*delivery.Delivery, error) {
	row, err := r.q.GetPool(ctx, GetPoolParams{
		Email:     email,
		MessageID: batchID.String(),
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, delivery.ErrDeliveryNotFound
		}
		return nil, err
	}
	return rowToDelivery(row), nil
}

func (r *deliveryRepository) SetScheduled(ctx context.Context, batchID batch.ID, email string) error {
	return r.q.SetSendingPoolScheduled(ctx, SetSendingPoolScheduledParams{
		Email:     email,
		MessageID: batchID.String(),
	})
}

func (r *deliveryRepository) Reschedule(ctx context.Context, batchID batch.ID, email string) error {
	d, err := r.Get(ctx, batchID, email)
	if err != nil {
		return err
	}
	return r.q.ReschedulePool(ctx, ReschedulePoolParams{
		Email:         email,
		MessageID:     batchID.String(),
		ScheduledTime: PgTimestampFromTime(d.NextRetryAt()),
	})
}

func (r *deliveryRepository) Clean(ctx context.Context, batchID batch.ID, email string) error {
	return r.q.CleanPool(ctx, CleanPoolParams{
		Email:     email,
		MessageID: batchID.String(),
	})
}

func rowsToDeliveries(rows []SendingPoolEmail) []*delivery.Delivery {
	out := make([]*delivery.Delivery, len(rows))
	for i, r := range rows {
		out[i] = rowToDelivery(r)
	}
	return out
}

func rowToDelivery(row SendingPoolEmail) *delivery.Delivery {
	return delivery.Load(delivery.LoadParams{
		BatchID:               batch.ID(row.MessageID),
		Email:                 row.Email,
		Fields:                fromCustomFields(row.Fields),
		SendAttempts:          int(row.SendAttemptsCnt),
		Domain:                row.Domain,
		ScheduledTime:         row.ScheduledTime.Time,
		OriginalScheduledTime: row.OriginalScheduledTime.Time,
	})
}

func toCustomFields(m map[string]string) CustomFields {
	if m == nil {
		return CustomFields{}
	}
	out := make(CustomFields, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

func fromCustomFields(f CustomFields) map[string]string {
	if f == nil {
		return nil
	}
	out := make(map[string]string, len(f))
	for k, v := range f {
		out[k] = v
	}
	return out
}
