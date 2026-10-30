-- migrate:up
ALTER TABLE api_keys ADD COLUMN roles text[];
CREATE INDEX api_keys_domain_created_id_idx ON api_keys(domain, created_at DESC, id DESC);

-- migrate:down
DROP INDEX api_keys_domain_created_id_idx;
ALTER TABLE api_keys DROP COLUMN roles;
