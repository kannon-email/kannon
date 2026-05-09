package sqlc

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/kannon-email/kannon/internal/batch"
	"github.com/kannon-email/kannon/internal/delivery"
)

type deliveryRepository struct {
	db      *pgxpool.Pool
	backoff delivery.BackoffPolicy
}

// NewDeliveryRepository creates a new PostgreSQL-backed Delivery repository.
// It writes to and reads from the sending_pool_emails table. The backoff
// policy is applied to every Delivery rehydrated from a row, so the
// repository's reschedule path uses the canonical curve threaded through the
// Container (see x/container.Container.BackoffPolicy).
func NewDeliveryRepository(db *pgxpool.Pool, backoff delivery.BackoffPolicy) delivery.Repository {
	return &deliveryRepository{db: db, backoff: backoff}
}

func (r *deliveryRepository) Schedule(ctx context.Context, ds ...*delivery.Delivery) error {
	if len(ds) == 0 {
		return nil
	}

	rows := make([]CreatePoolParams, len(ds))
	for i, d := range ds {
		ts := PgTimestampFromTime(d.ScheduledTime())
		rows[i] = CreatePoolParams{
			Email:                 d.Email(),
			Status:                SendingPoolStatusToValidate,
			ScheduledTime:         ts,
			OriginalScheduledTime: ts,
			MessageID:             d.BatchID().String(),
			Fields:                toCustomFields(d.Fields()),
			Domain:                d.Domain(),
		}
	}

	q := New(r.db)
	if _, err := q.CreatePool(ctx, rows); err != nil {
		return err
	}
	return nil
}

func (r *deliveryRepository) PrepareForSend(ctx context.Context, max int) ([]*delivery.Delivery, error) {
	q := New(r.db)
	rows, err := q.PrepareForSend(ctx, int32(max))
	if err != nil {
		return nil, err
	}
	return r.rowsToDeliveries(rows), nil
}

func (r *deliveryRepository) PrepareForValidate(ctx context.Context, max int) ([]*delivery.Delivery, error) {
	q := New(r.db)
	rows, err := q.PrepareForValidate(ctx, int32(max))
	if err != nil {
		return nil, err
	}
	return r.rowsToDeliveries(rows), nil
}

func (r *deliveryRepository) Get(ctx context.Context, batchID batch.ID, email string) (*delivery.Delivery, error) {
	q := New(r.db)
	row, err := q.GetPool(ctx, GetPoolParams{
		Email:     email,
		MessageID: batchID.String(),
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, delivery.ErrDeliveryNotFound
		}
		return nil, err
	}
	return r.rowToDelivery(row), nil
}

func (r *deliveryRepository) SetScheduled(ctx context.Context, batchID batch.ID, email string) error {
	q := New(r.db)
	return q.SetSendingPoolScheduled(ctx, SetSendingPoolScheduledParams{
		Email:     email,
		MessageID: batchID.String(),
	})
}

func (r *deliveryRepository) Reschedule(ctx context.Context, batchID batch.ID, email string) error {
	d, err := r.Get(ctx, batchID, email)
	if err != nil {
		return err
	}
	q := New(r.db)
	return q.ReschedulePool(ctx, ReschedulePoolParams{
		Email:         email,
		MessageID:     batchID.String(),
		ScheduledTime: PgTimestampFromTime(d.NextRetryAt()),
	})
}

func (r *deliveryRepository) Clean(ctx context.Context, batchID batch.ID, email string) error {
	q := New(r.db)
	return q.CleanPool(ctx, CleanPoolParams{
		Email:     email,
		MessageID: batchID.String(),
	})
}

func (r *deliveryRepository) rowsToDeliveries(rows []SendingPoolEmail) []*delivery.Delivery {
	out := make([]*delivery.Delivery, len(rows))
	for i, row := range rows {
		out[i] = r.rowToDelivery(row)
	}
	return out
}

func (r *deliveryRepository) rowToDelivery(row SendingPoolEmail) *delivery.Delivery {
	return delivery.Load(delivery.LoadParams{
		BatchID:               batch.ID(row.MessageID),
		Email:                 row.Email,
		Fields:                fromCustomFields(row.Fields),
		SendAttempts:          int(row.SendAttemptsCnt),
		Domain:                row.Domain,
		ScheduledTime:         row.ScheduledTime.Time,
		OriginalScheduledTime: row.OriginalScheduledTime.Time,
		Backoff:               r.backoff,
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
