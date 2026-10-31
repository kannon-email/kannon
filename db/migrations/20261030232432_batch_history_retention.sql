-- migrate:up
-- Keep the minimal idempotency receipt bound even after owned history is gone.
ALTER TABLE batch_intake_receipts DROP CONSTRAINT batch_intake_receipts_batch_id_fkey;
CREATE INDEX batch_history_expiration ON batch_intake_receipts(completed_at) WHERE completed_at IS NOT NULL;

-- migrate:down
DROP INDEX batch_history_expiration;
-- Expired receipts cannot regain a foreign key to already removed history.
DELETE FROM batch_intake_receipts r WHERE NOT EXISTS (SELECT 1 FROM messages m WHERE m.message_id=r.batch_id);
ALTER TABLE batch_intake_receipts ADD CONSTRAINT batch_intake_receipts_batch_id_fkey FOREIGN KEY(batch_id) REFERENCES messages(message_id);
