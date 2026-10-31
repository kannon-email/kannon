-- name: ReadInlineBatch :one
SELECT r.batch_id,r.domain,r.created_at,r.scheduled_at,r.completed_at,r.accepted_count,r.rejected_count,m.subject,m.sender_email,m.sender_alias,c.html,c.source_template_id,m.tracking,r.attachments,m.headers,COALESCE((SELECT jsonb_object_agg(status,n) FROM (SELECT status,count(*) n FROM delivery_intake_facts WHERE batch_id=r.batch_id GROUP BY status) counts),'{}'::jsonb)::jsonb AS counts FROM batch_intake_receipts r JOIN messages m ON m.message_id=r.batch_id JOIN batch_content_snapshots c ON c.batch_id=r.batch_id WHERE r.domain=$1 AND r.batch_id=$2 AND (r.completed_at IS NULL OR r.completed_at > $3::timestamptz);

-- name: ListInlineBatches :many
SELECT r.batch_id,r.domain,r.created_at,r.scheduled_at,r.completed_at,r.accepted_count,r.rejected_count,m.subject,m.sender_email,m.sender_alias,c.html,c.source_template_id,m.tracking,r.attachments,m.headers,COALESCE((SELECT jsonb_object_agg(status,n) FROM (SELECT status,count(*) n FROM delivery_intake_facts WHERE batch_id=r.batch_id GROUP BY status) counts),'{}'::jsonb)::jsonb AS counts FROM batch_intake_receipts r JOIN messages m ON m.message_id=r.batch_id JOIN batch_content_snapshots c ON c.batch_id=r.batch_id WHERE r.domain=$1 AND (r.completed_at IS NULL OR r.completed_at > $5::timestamptz) AND ($2='' OR (r.created_at,r.batch_id)<($3,$2)) ORDER BY r.created_at DESC,r.batch_id DESC LIMIT $4;

-- name: InlineBatchVisible :one
SELECT EXISTS(SELECT 1 FROM batch_intake_receipts r JOIN messages m ON m.message_id=r.batch_id WHERE r.domain=$1 AND r.batch_id=$2 AND (r.completed_at IS NULL OR r.completed_at > $3::timestamptz));

-- name: GetInlineDelivery :one
SELECT f.id,f.batch_id,r.domain,f.email,f.status,r.created_at,f.scheduled_at,f.updated_at,f.attempt_count,f.tracking,f.error_code,f.error_detail FROM delivery_intake_facts f JOIN batch_intake_receipts r ON r.batch_id=f.batch_id WHERE r.domain=$1 AND f.batch_id=$2 AND f.id=$3 AND (r.completed_at IS NULL OR r.completed_at > $4::timestamptz);

-- name: CleanupExpiredBatchHistory :exec
-- One statement keeps ownership checks and deletion atomic. Lock receipts in
-- the same order used by completion so concurrent outcomes cannot race cleanup.
WITH expired AS MATERIALIZED (
 SELECT r.batch_id,m.template_id FROM batch_intake_receipts r
 JOIN messages m ON m.message_id=r.batch_id
 WHERE r.completed_at <= $1::timestamptz
 AND NOT EXISTS (SELECT 1 FROM delivery_intake_facts f WHERE f.batch_id=r.batch_id AND f.outcome_at IS NULL)
 AND NOT EXISTS (SELECT 1 FROM sending_pool_emails p WHERE p.message_id=r.batch_id)
 ORDER BY r.batch_id FOR UPDATE OF r,m
 ), facts AS (DELETE FROM delivery_intake_facts WHERE batch_id IN (SELECT batch_id FROM expired)),
 snapshots AS (DELETE FROM batch_content_snapshots WHERE batch_id IN (SELECT batch_id FROM expired)),
 receipts AS (UPDATE batch_intake_receipts SET attachments='[]' WHERE batch_id IN (SELECT batch_id FROM expired)),
 removed AS (DELETE FROM messages WHERE message_id IN (SELECT batch_id FROM expired) RETURNING template_id)
 DELETE FROM templates t WHERE t.type='transient' AND t.template_id IN (SELECT template_id FROM removed)
 AND NOT EXISTS (SELECT 1 FROM messages m WHERE m.template_id=t.template_id AND m.message_id NOT IN (SELECT batch_id FROM expired));

-- name: CleanupExpiredMailerBatches :exec
-- Mailer API Batches have no receipt and so no completion time. Once the Pool holds
-- none of their Deliveries nothing reads the row again (only dispatch joins it),
-- so the clock counts from intake: a Batch still draining when it passes the
-- cutoff goes as soon as its last Delivery leaves the Pool. Receipted (REST)
-- Batches are left to CleanupExpiredBatchHistory.
WITH expired AS MATERIALIZED (
 SELECT m.message_id,m.template_id FROM messages m
 WHERE m.created_at <= $1::timestamptz
 AND NOT EXISTS (SELECT 1 FROM batch_intake_receipts r WHERE r.batch_id=m.message_id)
 AND NOT EXISTS (SELECT 1 FROM sending_pool_emails p WHERE p.message_id=m.message_id)
 ORDER BY m.message_id FOR UPDATE OF m
 ), removed AS (DELETE FROM messages WHERE message_id IN (SELECT message_id FROM expired) RETURNING template_id)
 DELETE FROM templates t WHERE t.type='transient' AND t.template_id IN (SELECT template_id FROM removed)
 AND NOT EXISTS (SELECT 1 FROM messages m WHERE m.template_id=t.template_id AND m.message_id NOT IN (SELECT message_id FROM expired));

-- name: ListInlineDeliveries :many
SELECT f.id,f.batch_id,r.domain,f.email,f.status,r.created_at,f.scheduled_at,f.updated_at FROM delivery_intake_facts f JOIN batch_intake_receipts r ON r.batch_id=f.batch_id WHERE r.domain=$1 AND f.batch_id=$2 AND f.id>$3 AND ($4='' OR f.status=$4) ORDER BY f.id LIMIT $5;

-- name: LockInlineKey :one
SELECT pg_try_advisory_xact_lock(hashtextextended($1,0));

-- name: ReadInlineReceipt :one
SELECT batch_id,domain,created_at,accepted_count,rejected_count,fingerprint,scheduled_at,attachments FROM batch_intake_receipts WHERE domain=$1 AND idempotency_key=$2;

-- name: InsertInlineReceipt :exec
INSERT INTO batch_intake_receipts(domain,idempotency_key,fingerprint,batch_id,created_at,accepted_count,rejected_count,scheduled_at,attachments) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9);

-- name: InsertInlineSnapshot :exec
INSERT INTO batch_content_snapshots(batch_id,html,source_template_id) VALUES($1,$2,$3);

-- name: InsertInlineDelivery :exec
INSERT INTO delivery_intake_facts(id,batch_id,email,tracking) VALUES($1,$2,$3,$4);

-- name: ListPublicEvents :many
SELECT id,type,email,message_id,domain,timestamp,data FROM stats WHERE domain=$1 AND timestamp >= $2 AND timestamp < $3 AND type::text=ANY($4::text[]) AND ($5='' OR message_id=$5) AND ($6::int=0 OR (timestamp,id)<($7,$6)) AND NOT (type::text IN ('opened','clicked') AND (email = '' OR email = 'anonymous@track.' || domain OR COALESCE(data->>'trackingMode','') = 'anonymous')) ORDER BY timestamp DESC,id DESC LIMIT $8;
