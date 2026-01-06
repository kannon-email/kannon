-- migrate:up
ALTER TABLE domains DROP COLUMN key;

-- migrate:down
ALTER TABLE domains ADD COLUMN key VARCHAR NOT NULL DEFAULT '';
