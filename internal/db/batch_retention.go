package sqlc

import (
	"context"
	"time"
)

// CleanupBatchHistory removes completed REST Batches after exactly 30 days.
// Receipts remain bound independently; Stats and shared Templates are untouched.
func (q *Queries) CleanupBatchHistory(ctx context.Context, now time.Time) error {
	// One statement keeps ownership checks and deletion atomic. Lock receipts in
	// the same order used by completion so concurrent outcomes cannot race cleanup.
	_, err := q.db.Exec(ctx, `WITH expired AS MATERIALIZED (
 SELECT r.batch_id,m.template_id FROM batch_intake_receipts r
 JOIN messages m ON m.message_id=r.batch_id
 WHERE r.completed_at <= $1::timestamptz - interval '720 hours'
 AND NOT EXISTS (SELECT 1 FROM delivery_intake_facts f WHERE f.batch_id=r.batch_id AND f.outcome_at IS NULL)
 AND NOT EXISTS (SELECT 1 FROM sending_pool_emails p WHERE p.message_id=r.batch_id)
 ORDER BY r.batch_id FOR UPDATE OF r,m
 ), facts AS (DELETE FROM delivery_intake_facts WHERE batch_id IN (SELECT batch_id FROM expired)),
 snapshots AS (DELETE FROM batch_content_snapshots WHERE batch_id IN (SELECT batch_id FROM expired)),
 receipts AS (UPDATE batch_intake_receipts SET attachments='[]' WHERE batch_id IN (SELECT batch_id FROM expired)),
 removed AS (DELETE FROM messages WHERE message_id IN (SELECT batch_id FROM expired) RETURNING template_id)
 DELETE FROM templates t WHERE t.type='transient' AND t.template_id IN (SELECT template_id FROM removed)
 AND NOT EXISTS (SELECT 1 FROM messages m WHERE m.template_id=t.template_id AND m.message_id NOT IN (SELECT batch_id FROM expired))`, now)
	return err
}
