-- migrate:up
ALTER TABLE sending_pool_emails ADD COLUMN fields jsonb NOT NULL DEFAULT '{}';

-- migrate:down

ALTER TABLE sending_pool_emails DROP COLUMN fields;
