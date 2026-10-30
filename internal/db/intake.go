package sqlc

import (
	"context"
	"errors"
	"log/slog"

	"github.com/jackc/pgx/v5"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/kannon-email/kannon/internal/batch"
	"github.com/kannon-email/kannon/internal/delivery"
	"github.com/kannon-email/kannon/internal/intake"
	"github.com/kannon-email/kannon/internal/templates"
)

type intakeStore struct {
	*templatesRepository
	pool *pgxpool.Pool
}

// NewIntakeStore persists a complete Batch acceptance in one PostgreSQL transaction.
func NewIntakeStore(pool *pgxpool.Pool) intake.Store {
	return &intakeStore{templatesRepository: &templatesRepository{db: pool}, pool: pool}
}

func (s *intakeStore) Accept(ctx context.Context, tpl *templates.Template, b *batch.Batch, ds []*delivery.Delivery) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() {
		if err := tx.Rollback(ctx); err != nil && !errors.Is(err, pgx.ErrTxClosed) {
			slog.Error("cannot roll back batch acceptance", "err", err)
		}
	}()
	if tpl != nil {
		if err := (&templatesRepository{db: tx}).Create(ctx, tpl); err != nil {
			return err
		}
	}
	if err := (&batchRepository{db: tx}).Create(ctx, b); err != nil {
		return err
	}
	if err := (&deliveryRepository{db: tx}).Schedule(ctx, ds...); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
