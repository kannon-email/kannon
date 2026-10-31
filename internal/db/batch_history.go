package sqlc

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/kannon-email/kannon/internal/intake"
	"github.com/kannon-email/kannon/internal/keyset"
	"github.com/kannon-email/kannon/internal/values"
	"time"
)

func (s *inlineStore) ReadBatch(ctx context.Context, d values.DomainName, id string) (*intake.BatchHistory, error) {
	row, err := New(s.pool).ReadInlineBatch(ctx, ReadInlineBatchParams{Domain: d.String(), BatchID: id, Column3: s.historyCutoff()})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, intake.ErrBatchNotFound
	}
	if err != nil {
		return nil, err
	}
	return batchHistoryFromRow(row)
}
func (s *inlineStore) ListBatches(ctx context.Context, d values.DomainName, page keyset.Page[intake.BatchPosition]) ([]intake.BatchHistory, error) {
	var after intake.BatchPosition
	if page.After != nil {
		after = *page.After
	}
	rows, err := New(s.pool).ListInlineBatches(ctx, ListInlineBatchesParams{Domain: d.String(), Column2: after.ID, CreatedAt: timestamptz(after.CreatedAt), Limit: int32(page.Limit), Column5: s.historyCutoff()})
	if err != nil {
		return nil, err
	}
	out := make([]intake.BatchHistory, 0, len(rows))
	for _, row := range rows {
		b, err := batchHistoryFromRow(ReadInlineBatchRow(row))
		if err != nil {
			return nil, err
		}
		out = append(out, *b)
	}
	return out, nil
}
func batchHistoryFromRow(row ReadInlineBatchRow) (*intake.BatchHistory, error) {
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
	b.Headers = fromSQLCHeaders(row.Headers)
	b.OneClickUnsubscribe = fromSQLCUnsubscribe(row.Headers)
	return b, nil
}
func (s *inlineStore) ListDeliveries(ctx context.Context, d values.DomainName, id string, status intake.DeliveryStatus, page keyset.Page[string]) ([]intake.DeliveryHistory, error) {
	visible, err := New(s.pool).InlineBatchVisible(ctx, InlineBatchVisibleParams{Domain: d.String(), BatchID: id, Column3: s.historyCutoff()})
	if err != nil {
		return nil, err
	}
	if !visible {
		return nil, intake.ErrBatchNotFound
	}
	after := ""
	if page.After != nil {
		after = *page.After
	}
	rows, err := New(s.pool).ListInlineDeliveries(ctx, ListInlineDeliveriesParams{Domain: d.String(), BatchID: id, ID: after, Column4: string(status), Limit: int32(page.Limit)})
	if err != nil {
		return nil, err
	}
	out := []intake.DeliveryHistory{}
	for _, row := range rows {
		out = append(out, intake.DeliveryHistory{ID: row.ID, BatchID: row.BatchID, Domain: row.Domain, Email: row.Email, Status: intake.DeliveryStatus(row.Status), CreatedAt: row.CreatedAt.Time, ScheduledAt: row.ScheduledAt.Time, UpdatedAt: row.UpdatedAt.Time})
	}
	return out, nil
}

// historyCutoff hides Batches completed longer than intake.HistoryRetention ago.
func (s *inlineStore) historyCutoff() pgtype.Timestamptz {
	return timestamptz(intake.HistoryCutoff(s.now()))
}
func timestamptz(t time.Time) pgtype.Timestamptz { return pgtype.Timestamptz{Time: t, Valid: true} }
