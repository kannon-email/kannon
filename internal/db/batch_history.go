package sqlc

import (
	"context"
	"errors"
	"github.com/jackc/pgx/v5"
	"github.com/kannon-email/kannon/internal/intake"
	"github.com/kannon-email/kannon/internal/values"
)

func (s *inlineStore) ReadBatch(ctx context.Context, d values.DomainName, id string) (*intake.BatchHistory, error) {
	b := &intake.BatchHistory{}
	err := s.pool.QueryRow(ctx, `SELECT r.batch_id,r.domain,r.created_at,r.scheduled_at,r.completed_at,r.accepted_count,r.rejected_count,m.subject,m.sender_email,m.sender_alias,c.html,m.tracking,r.attachments,m.headers FROM batch_intake_receipts r JOIN messages m ON m.message_id=r.batch_id JOIN batch_content_snapshots c ON c.batch_id=r.batch_id WHERE r.domain=$1 AND r.batch_id=$2`, d.String(), id).Scan(&b.ID, &b.Domain, &b.CreatedAt, &b.ScheduledAt, &b.CompletedAt, &b.AcceptedCount, &b.RejectedCount, &b.Subject, &b.Sender.Email, &b.Sender.Alias, &b.HTML, &b.Tracking, &b.Attachments, &b.Headers)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, intake.ErrDeliveryNotFound
	}
	if err != nil {
		return nil, err
	}
	rows, err := s.pool.Query(ctx, `SELECT status,count(*) FROM delivery_intake_facts WHERE batch_id=$1 GROUP BY status`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	b.Counts = map[string]int{}
	for rows.Next() {
		var st string
		var n int
		if err = rows.Scan(&st, &n); err != nil {
			return nil, err
		}
		b.Counts[st] = n
	}
	return b, rows.Err()
}
func (s *inlineStore) ListBatches(ctx context.Context, d values.DomainName, q intake.HistoryQuery) ([]intake.BatchHistory, error) {
	rows, err := s.pool.Query(ctx, `SELECT batch_id FROM batch_intake_receipts WHERE domain=$1 AND ($2='' OR (created_at,batch_id)<($3,$2)) ORDER BY created_at DESC,batch_id DESC LIMIT $4`, d.String(), q.AfterID, q.AfterTime, q.Limit)
	if err != nil {
		return nil, err
	}
	var ids []string
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		ids = append(ids, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	out := []intake.BatchHistory{}
	for _, id := range ids {
		b, err := s.ReadBatch(ctx, d, id)
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
	rows, err := s.pool.Query(ctx, `SELECT f.id,f.batch_id,r.domain,f.email,f.status,r.created_at,f.scheduled_at,f.updated_at FROM delivery_intake_facts f JOIN batch_intake_receipts r ON r.batch_id=f.batch_id WHERE r.domain=$1 AND f.batch_id=$2 AND f.id>$3 AND ($4='' OR f.status=$4) ORDER BY f.id LIMIT $5`, d.String(), id, q.AfterID, q.Status, q.Limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []intake.DeliveryHistory{}
	for rows.Next() {
		var v intake.DeliveryHistory
		if err = rows.Scan(&v.ID, &v.BatchID, &v.Domain, &v.Email, &v.Status, &v.CreatedAt, &v.ScheduledAt, &v.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}
