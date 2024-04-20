-- migrate:up
ALTER TABLE messages ADD COLUMN attachments JSONB;

-- migrate:down

ALTER TABLE messages DROP COLUMN attachments;