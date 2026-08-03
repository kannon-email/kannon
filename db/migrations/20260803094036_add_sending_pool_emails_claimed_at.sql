-- migrate:up
-- claimed_at records when a worker claimed a Delivery into an in-flight status.
-- It is the honest answer to "how long has this row been in flight": the claim
-- does not touch scheduled_time, so under a backlog that column can be hours in
-- the past on a row claimed a second ago (ADR 0007).
ALTER TABLE sending_pool_emails ADD COLUMN claimed_at timestamp NULL;

-- Rows already in flight when this migration runs were claimed by a binary that
-- did not write the column. NOW() gives each of them one full threshold as grace
-- before the owning worker reclaims it, instead of leaving them NULL and
-- therefore stranded forever.
UPDATE sending_pool_emails SET claimed_at = NOW()
  WHERE status IN ('sending', 'validating');

CREATE INDEX sending_pool_emails_status_claimed_at_idx
  ON sending_pool_emails (status, claimed_at)
  WHERE status IN ('sending', 'validating');

-- migrate:down
DROP INDEX sending_pool_emails_status_claimed_at_idx;

ALTER TABLE sending_pool_emails DROP COLUMN claimed_at;
