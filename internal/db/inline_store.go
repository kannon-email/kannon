package sqlc

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/kannon-email/kannon/internal/batch"
	"github.com/kannon-email/kannon/internal/intake"
	"github.com/kannon-email/kannon/internal/values"
	"log/slog"
	"time"
)

type inlineStore struct {
	pool *pgxpool.Pool
	now  func() time.Time
	*templatesRepository
}

func NewInlineStore(pool *pgxpool.Pool, clock ...func() time.Time) intake.InlineStore {
	now := time.Now
	if len(clock) > 0 {
		now = clock[0]
	}
	return &inlineStore{pool: pool, now: now, templatesRepository: &templatesRepository{db: pool}}
}
func (s *inlineStore) CreateInline(ctx context.Context, domain values.DomainName, key, fingerprint string, build func() (*intake.InlineWork, error)) (*intake.InlineReceipt, bool, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, false, err
	}
	defer func() {
		if err := tx.Rollback(context.Background()); err != nil && !errors.Is(err, pgx.ErrTxClosed) {
			slog.Error("cannot roll back inline intake", "err", err)
		}
	}()
	locked, err := New(tx).LockInlineKey(ctx, domain.String()+":"+key)
	if err != nil {
		return nil, false, err
	}
	if !locked {
		return nil, false, intake.ErrIdempotencyInProgress
	}
	r := &intake.InlineReceipt{}
	row, err := New(tx).ReadInlineReceipt(ctx, ReadInlineReceiptParams{Domain: domain.String(), IdempotencyKey: key})
	if err == nil {
		if row.Fingerprint != fingerprint {
			return nil, false, intake.ErrIdempotencyConflict
		}
		r = &intake.InlineReceipt{ID: row.BatchID, Domain: row.Domain, CreatedAt: row.CreatedAt.Time.UTC(), ScheduledAt: row.ScheduledAt.Time.UTC(), AcceptedCount: int(row.AcceptedCount), RejectedCount: int(row.RejectedCount)}
		if err := json.Unmarshal(row.Attachments, &r.Attachments); err != nil {
			return nil, false, err
		}
		return r, true, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return nil, false, err
	}
	work, err := build()
	if err != nil {
		return nil, false, err
	}
	if err = (&templatesRepository{db: tx}).Create(ctx, work.Template); err != nil {
		return nil, false, err
	}
	if err = (&batchRepository{db: tx}).Create(ctx, work.Batch); err != nil {
		return nil, false, err
	}
	if err = (&deliveryRepository{db: tx}).Schedule(ctx, work.Deliveries...); err != nil {
		return nil, false, err
	}
	r = &work.Receipt
	metadata, err := json.Marshal(r.Attachments)
	if err != nil {
		return nil, false, err
	}
	err = New(tx).InsertInlineReceipt(ctx, InsertInlineReceiptParams{Domain: r.Domain, IdempotencyKey: key, Fingerprint: fingerprint, BatchID: r.ID, CreatedAt: timestamptz(r.CreatedAt), AcceptedCount: int32(r.AcceptedCount), RejectedCount: int32(r.RejectedCount), ScheduledAt: timestamptz(r.ScheduledAt), Attachments: metadata})
	if err != nil {
		return nil, false, err
	}
	err = New(tx).InsertInlineSnapshot(ctx, InsertInlineSnapshotParams{BatchID: r.ID, Html: work.Template.Html(), SourceTemplateID: work.SourceTemplateID})
	if err != nil {
		return nil, false, err
	}
	for _, d := range work.Deliveries {
		err = New(tx).InsertInlineDelivery(ctx, InsertInlineDeliveryParams{ID: batch.NewID(r.Domain).String(), BatchID: r.ID, Email: d.Email(), Tracking: d.TrackingPolicy()})
		if err != nil {
			return nil, false, err
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return nil, false, err
	}
	return r, false, nil
}
