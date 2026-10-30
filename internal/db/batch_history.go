package sqlc

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/kannon-email/kannon/internal/intake"
	"github.com/kannon-email/kannon/internal/values"
	"time"
)

func (s *inlineStore) ReadBatch(ctx context.Context, d values.DomainName, id string) (*intake.BatchHistory, error) {
	row, err := New(s.pool).ReadInlineBatch(ctx, ReadInlineBatchParams{Domain: d.String(), BatchID: id, Column3: timestamptz(s.now())})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, intake.ErrDeliveryNotFound
	}
	if err != nil {
		return nil, err
	}
	b := &intake.BatchHistory{ID: row.BatchID, Domain: row.Domain, CreatedAt: row.CreatedAt.Time, ScheduledAt: row.ScheduledAt.Time, AcceptedCount: int(row.AcceptedCount), RejectedCount: int(row.RejectedCount), Subject: row.Subject, HTML: row.Html, SourceTemplateID: row.SourceTemplateID, Tracking: row.Tracking}
	b.Sender.Email = row.SenderEmail
	b.Sender.Alias = row.SenderAlias
	if row.CompletedAt.Valid {
		b.CompletedAt = &row.CompletedAt.Time
	}
	if err := json.Unmarshal(row.Attachments, &b.Attachments); err != nil {
		return nil, err
	}
	if err := json.Unmarshal(row.Counts, &b.Counts); err != nil {
		return nil, err
	}
	b.Headers, err = json.Marshal(row.Headers)
	if err != nil {
		return nil, err
	}
	return b, nil
}
func (s *inlineStore) ListBatches(ctx context.Context, d values.DomainName, q intake.HistoryQuery) ([]intake.BatchHistory, error) {
	ids, err := New(s.pool).ListInlineBatchIDs(ctx, ListInlineBatchIDsParams{Domain: d.String(), Column2: q.AfterID, CreatedAt: timestamptz(q.AfterTime), Limit: int32(q.Limit), Column5: timestamptz(s.now())})
	if err != nil {
		return nil, err
	}
	out := []intake.BatchHistory{}
	for _, id := range ids {
		b, err := s.ReadBatch(ctx, d, id)
		if errors.Is(err, intake.ErrDeliveryNotFound) {
			continue
		}
		if err != nil {
			return nil, err
		}
		out = append(out, *b)
	}
	return out, nil
}
func (s *inlineStore) ListDeliveries(ctx context.Context, d values.DomainName, id string, q intake.HistoryQuery) ([]intake.DeliveryHistory, error) {
	if _, err := s.ReadBatch(ctx, d, id); err != nil {
		return nil, err
	}
	rows, err := New(s.pool).ListInlineDeliveries(ctx, ListInlineDeliveriesParams{Domain: d.String(), BatchID: id, ID: q.AfterID, Column4: q.Status, Limit: int32(q.Limit)})
	if err != nil {
		return nil, err
	}
	out := []intake.DeliveryHistory{}
	for _, row := range rows {
		out = append(out, intake.DeliveryHistory{ID: row.ID, BatchID: row.BatchID, Domain: row.Domain, Email: row.Email, Status: row.Status, CreatedAt: row.CreatedAt.Time, ScheduledAt: row.ScheduledAt.Time, UpdatedAt: row.UpdatedAt.Time})
	}
	return out, nil
}
func timestamptz(t time.Time) pgtype.Timestamptz { return pgtype.Timestamptz{Time: t, Valid: true} }
