-- migrate:up
-- For the retention sweep: the primary key leads with domain.
CREATE INDEX aggregated_stats_timestamp_idx ON aggregated_stats (timestamp);

-- migrate:down
DROP INDEX aggregated_stats_timestamp_idx;
