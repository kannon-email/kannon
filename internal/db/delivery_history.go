package sqlc

import (
	"context"
	"errors"
	"github.com/jackc/pgx/v5"
	"github.com/kannon-email/kannon/internal/intake"
	"github.com/kannon-email/kannon/internal/values"
)

func (s *inlineStore) GetDelivery(ctx context.Context, domain values.DomainName, batchID, id string) (*intake.DeliveryHistory, error) {
	d := &intake.DeliveryHistory{}
	err := s.pool.QueryRow(ctx, `SELECT f.id,f.batch_id,r.domain,f.email,f.status,r.created_at,f.scheduled_at,f.updated_at,f.attempt_count,f.tracking,f.error_code,f.error_detail FROM delivery_intake_facts f JOIN batch_intake_receipts r ON r.batch_id=f.batch_id WHERE r.domain=$1 AND f.batch_id=$2 AND f.id=$3 AND (r.completed_at IS NULL OR r.completed_at > $4::timestamptz - interval '720 hours')`, domain.String(), batchID, id, s.now()).Scan(&d.ID, &d.BatchID, &d.Domain, &d.Email, &d.Status, &d.CreatedAt, &d.ScheduledAt, &d.UpdatedAt, &d.AttemptCount, &d.Tracking, &d.ErrorCode, &d.ErrorDetail)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, intake.ErrDeliveryNotFound
	}
	return d, err
}
