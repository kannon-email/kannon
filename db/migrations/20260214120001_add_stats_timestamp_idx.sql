-- migrate:up
CREATE INDEX stats_timestamp_idx ON stats (timestamp);

-- migrate:down
DROP INDEX stats_timestamp_idx;
