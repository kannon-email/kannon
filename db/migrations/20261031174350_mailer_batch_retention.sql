-- migrate:up
-- A Batch accepted by the Mailer API has no intake receipt, so nothing
-- records when it completed: its retention counts from intake instead. Existing
-- rows take the migration time and expire one retention period after upgrade.
ALTER TABLE messages ADD COLUMN created_at timestamptz NOT NULL DEFAULT now();
CREATE INDEX messages_created_at_idx ON messages(created_at);

-- migrate:down
DROP INDEX messages_created_at_idx;
ALTER TABLE messages DROP COLUMN created_at;
