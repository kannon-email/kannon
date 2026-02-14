-- migrate:up
CREATE EXTENSION IF NOT EXISTS pgcrypto;

ALTER TABLE api_keys ADD COLUMN key_hash VARCHAR(64);
ALTER TABLE api_keys ADD COLUMN key_prefix VARCHAR(10);

UPDATE api_keys
SET key_hash = encode(digest(key, 'sha256'), 'hex'),
    key_prefix = LEFT(key, 8);

ALTER TABLE api_keys ALTER COLUMN key_hash SET NOT NULL;
ALTER TABLE api_keys ALTER COLUMN key_prefix SET NOT NULL;

DROP INDEX api_keys_key_active_idx;
ALTER TABLE api_keys DROP COLUMN key;

ALTER TABLE api_keys ADD CONSTRAINT api_keys_key_hash_key UNIQUE (key_hash);
CREATE INDEX api_keys_key_hash_active_idx ON api_keys (key_hash) WHERE is_active = TRUE;

-- migrate:down
-- Lossy: cannot recover plaintext keys from hashes
ALTER TABLE api_keys ADD COLUMN key VARCHAR(512);
UPDATE api_keys SET key = 'LOST_' || key_prefix || '_MIGRATED';
ALTER TABLE api_keys ALTER COLUMN key SET NOT NULL;
ALTER TABLE api_keys ADD CONSTRAINT api_keys_key_key UNIQUE (key);
CREATE INDEX api_keys_key_active_idx ON api_keys (key) WHERE is_active = TRUE;
ALTER TABLE api_keys DROP COLUMN key_hash;
ALTER TABLE api_keys DROP COLUMN key_prefix;
