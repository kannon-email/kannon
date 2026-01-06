-- migrate:up
CREATE TABLE api_keys (
    id VARCHAR(512) PRIMARY KEY,
    key VARCHAR(512) NOT NULL UNIQUE,
    name VARCHAR(100) NOT NULL,
    domain VARCHAR(512) NOT NULL,
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    expires_at TIMESTAMP,
    is_active BOOLEAN NOT NULL DEFAULT TRUE,
    deactivated_at TIMESTAMP
);

-- Index for authentication lookups (most common query)
CREATE INDEX api_keys_key_active_idx ON api_keys (key) WHERE is_active = TRUE;

-- Index for listing keys by domain
CREATE INDEX api_keys_domain_idx ON api_keys (domain);

-- Index for finding expired keys (for potential cleanup jobs)
CREATE INDEX api_keys_expires_at_idx ON api_keys (expires_at) WHERE expires_at IS NOT NULL;

-- Foreign key constraint to ensure referential integrity
ALTER TABLE ONLY api_keys
    ADD CONSTRAINT api_keys_domain_fkey FOREIGN KEY (domain) REFERENCES domains(domain) ON DELETE CASCADE;

-- Migrate existing domain keys to new table
INSERT INTO api_keys (id, key, name, domain, created_at, expires_at, is_active)
SELECT
    'key_' || d.id::text,
     d.key,
    'default',
    d.domain,
    d.created_at,
    NULL,
    TRUE
FROM domains d;

-- migrate:down
DROP TABLE api_keys;
