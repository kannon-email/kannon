-- migrate:up

-- Domains and Deliveries always hold a concrete Tracking Policy. The default
-- both backfills existing rows and defaults new ones: existing installations
-- track effectively 'full' today, so 'identified' is a reduction that keeps
-- per-recipient attribution and stops retaining IP and user agent (ADR 0003).
ALTER TABLE domains ADD COLUMN tracking jsonb NOT NULL
  DEFAULT '{"opens":"identified","links":"identified"}'::jsonb;

ALTER TABLE sending_pool_emails ADD COLUMN tracking jsonb NOT NULL
  DEFAULT '{"opens":"identified","links":"identified"}'::jsonb;

-- The Batch keeps what the caller stated, as provenance only. It is the one
-- place an unstated Tracking Mode may be stored.
ALTER TABLE messages ADD COLUMN tracking jsonb NOT NULL DEFAULT '{}'::jsonb;

-- migrate:down
ALTER TABLE messages DROP COLUMN tracking;
ALTER TABLE sending_pool_emails DROP COLUMN tracking;
ALTER TABLE domains DROP COLUMN tracking;
