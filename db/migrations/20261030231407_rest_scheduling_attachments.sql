-- migrate:up
ALTER TABLE batch_intake_receipts ADD COLUMN scheduled_at timestamptz;
UPDATE batch_intake_receipts SET scheduled_at=created_at;
ALTER TABLE batch_intake_receipts ALTER COLUMN scheduled_at SET NOT NULL;
ALTER TABLE batch_intake_receipts ADD COLUMN attachments jsonb NOT NULL DEFAULT '[]';

-- migrate:down
ALTER TABLE batch_intake_receipts DROP COLUMN scheduled_at, DROP COLUMN attachments;
