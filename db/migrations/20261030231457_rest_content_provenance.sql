-- migrate:up
ALTER TABLE batch_content_snapshots ADD COLUMN source_template_id text NOT NULL DEFAULT '';
-- migrate:down
ALTER TABLE batch_content_snapshots DROP COLUMN source_template_id;
