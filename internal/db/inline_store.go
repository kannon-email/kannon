package sqlc

import (
	"context"
	"errors"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/kannon-email/kannon/internal/batch"
	"github.com/kannon-email/kannon/internal/intake"
	"github.com/kannon-email/kannon/internal/values"
	"log/slog"
)

type inlineStore struct {
	pool *pgxpool.Pool
	*templatesRepository
}

func NewInlineStore(pool *pgxpool.Pool) intake.InlineStore {
	return &inlineStore{pool: pool, templatesRepository: &templatesRepository{db: pool}}
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
	var locked bool
	err = tx.QueryRow(ctx, `SELECT pg_try_advisory_xact_lock(hashtextextended($1,0))`, domain.String()+":"+key).Scan(&locked)
	if err != nil {
		return nil, false, err
	}
	if !locked {
		return nil, false, intake.ErrIdempotencyInProgress
	}
	r := &intake.InlineReceipt{}
	var digest string
	err = tx.QueryRow(ctx, `SELECT batch_id,domain,created_at,accepted_count,rejected_count,fingerprint FROM batch_intake_receipts WHERE domain=$1 AND idempotency_key=$2`, domain.String(), key).Scan(&r.ID, &r.Domain, &r.CreatedAt, &r.AcceptedCount, &r.RejectedCount, &digest)
	if err == nil {
		r.CreatedAt = r.CreatedAt.UTC()
		if digest != fingerprint {
			return nil, false, intake.ErrIdempotencyConflict
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
	_, err = tx.Exec(ctx, `INSERT INTO batch_intake_receipts(domain,idempotency_key,fingerprint,batch_id,created_at,accepted_count,rejected_count) VALUES($1,$2,$3,$4,$5,$6,$7)`, r.Domain, key, fingerprint, r.ID, r.CreatedAt, r.AcceptedCount, r.RejectedCount)
	if err != nil {
		return nil, false, err
	}
	_, err = tx.Exec(ctx, `INSERT INTO batch_content_snapshots(batch_id,html,source_template_id) VALUES($1,$2,$3)`, r.ID, work.Template.Html(), work.SourceTemplateID)
	if err != nil {
		return nil, false, err
	}
	for _, d := range work.Deliveries {
		_, err = tx.Exec(ctx, `INSERT INTO delivery_intake_facts(id,batch_id,email,tracking) VALUES($1,$2,$3,$4)`, batch.NewID(r.Domain).String(), r.ID, d.Email(), d.TrackingPolicy())
		if err != nil {
			return nil, false, err
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return nil, false, err
	}
	return r, false, nil
}
