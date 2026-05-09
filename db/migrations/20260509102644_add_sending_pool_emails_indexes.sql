-- migrate:up
CREATE INDEX sending_pool_emails_status_scheduled_time_idx
  ON sending_pool_emails (status, scheduled_time)
  WHERE status IN ('to_validate', 'scheduled');

-- migrate:down
DROP INDEX sending_pool_emails_status_scheduled_time_idx;
