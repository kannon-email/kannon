package sqlc

import (
	"context"
	"errors"
	"github.com/jackc/pgx/v5"
	"github.com/kannon-email/kannon/internal/intake"
	"github.com/kannon-email/kannon/internal/values"
)

func (s *inlineStore) GetDelivery(ctx context.Context, domain values.DomainName, batchID, id string) (*intake.DeliveryHistory, error) {
	row, err := New(s.pool).GetInlineDelivery(ctx, GetInlineDeliveryParams{Domain: domain.String(), BatchID: batchID, ID: id, Column4: s.historyCutoff()})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, intake.ErrDeliveryNotFound
	}
	if err != nil {
		return nil, err
	}
	return &intake.DeliveryHistory{ID: row.ID, BatchID: row.BatchID, Domain: row.Domain, Email: row.Email, Status: intake.DeliveryStatus(row.Status), CreatedAt: row.CreatedAt.Time, ScheduledAt: row.ScheduledAt.Time, UpdatedAt: row.UpdatedAt.Time, AttemptCount: int(row.AttemptCount), Tracking: row.Tracking, ErrorCode: row.ErrorCode, ErrorDetail: row.ErrorDetail}, nil
}
