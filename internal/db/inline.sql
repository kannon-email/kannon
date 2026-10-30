-- name: ReadInlineBatch :one
SELECT r.batch_id,r.domain,r.created_at,r.scheduled_at,r.completed_at,r.accepted_count,r.rejected_count,m.subject,m.sender_email,m.sender_alias,c.html,c.source_template_id,m.tracking,r.attachments,m.headers,COALESCE((SELECT jsonb_object_agg(status,n) FROM (SELECT status,count(*) n FROM delivery_intake_facts WHERE batch_id=r.batch_id GROUP BY status) counts),'{}'::jsonb)::jsonb AS counts FROM batch_intake_receipts r JOIN messages m ON m.message_id=r.batch_id JOIN batch_content_snapshots c ON c.batch_id=r.batch_id WHERE r.domain=$1 AND r.batch_id=$2 AND (r.completed_at IS NULL OR r.completed_at > $3::timestamptz - interval '720 hours');

-- name: ListInlineBatchIDs :many
SELECT batch_id FROM batch_intake_receipts WHERE batch_intake_receipts.domain=$1 AND EXISTS (SELECT 1 FROM messages m WHERE m.message_id=batch_intake_receipts.batch_id) AND (completed_at IS NULL OR completed_at > $5::timestamptz - interval '720 hours') AND ($2='' OR (created_at,batch_id)<($3,$2)) ORDER BY created_at DESC,batch_id DESC LIMIT $4;

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
