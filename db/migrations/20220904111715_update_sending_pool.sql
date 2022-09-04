-- migrate:up
ALTER TABLE sending_pool_emails ADD COLUMN created_at TIMESTAMP NOT NULL DEFAULT NOW();
ALTER TABLE sending_pool_emails ADD COLUMN domain VARCHAR NOT NULL;
ALTER TABLE sending_pool_emails DROP COLUMN error_msg;
ALTER TABLE sending_pool_emails DROP COLUMN error_code;

-- migrate:down

ALTER TABLE sending_pool_emails ADD COLUMN error_msg VARCHAR NOT NULL DEFAULT '';
ALTER TABLE sending_pool_emails ADD COLUMN error_code int NOT NULL DEFAULT 0;

ALTER TABLE sending_pool_emails DROP COLUMN created_at;
ALTER TABLE sending_pool_emails DROP COLUMN domain;