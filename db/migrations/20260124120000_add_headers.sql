-- migrate:up
ALTER TABLE messages ADD COLUMN headers JSONB NOT NULL DEFAULT '{}';

-- migrate:down
ALTER TABLE messages DROP COLUMN headers;
