-- migrate:up
-- The columns held UTC wall-clock times: pgx encodes a timestamp by dropping
-- the offset, so an expiry sent in another zone was stored shifted by it.
-- timestamptz keeps the instant; existing values are read as UTC.
ALTER TABLE api_keys
    ALTER COLUMN created_at TYPE timestamptz USING created_at AT TIME ZONE 'UTC',
    ALTER COLUMN expires_at TYPE timestamptz USING expires_at AT TIME ZONE 'UTC',
    ALTER COLUMN deactivated_at TYPE timestamptz USING deactivated_at AT TIME ZONE 'UTC';

-- migrate:down
ALTER TABLE api_keys
    ALTER COLUMN created_at TYPE timestamp USING created_at AT TIME ZONE 'UTC',
    ALTER COLUMN expires_at TYPE timestamp USING expires_at AT TIME ZONE 'UTC',
    ALTER COLUMN deactivated_at TYPE timestamp USING deactivated_at AT TIME ZONE 'UTC';
