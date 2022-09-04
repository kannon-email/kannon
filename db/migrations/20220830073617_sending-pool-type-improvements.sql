-- migrate:up
ALTER TABLE sending_pool_emails ADD COLUMN new_status VARCHAR(100) NOT NULL DEFAULT 'initializing';
UPDATE sending_pool_emails SET new_status=status;
ALTER TABLE sending_pool_emails DROP COLUMN status;
ALTER TABLE sending_pool_emails RENAME COLUMN new_status TO status;

DROP TYPE SENDING_POOL_STATUS;

-- migrate:down

CREATE TYPE SENDING_POOL_STATUS AS ENUM (
    'initializing',
    'sending',
    'sent',
    'scheduled',
    'error'
);

ALTER TABLE sending_pool_emails ADD COLUMN new_status SENDING_POOL_STATUS NOT NULL DEFAULT 'initializing';
UPDATE sending_pool_emails SET new_status = status::SENDING_POOL_STATUS;
ALTER TABLE sending_pool_emails DROP COLUMN status;
ALTER TABLE sending_pool_emails RENAME COLUMN new_status TO status;