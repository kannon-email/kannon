-- name: IncrementAggregatedStat :exec
INSERT INTO aggregated_stats (domain, timestamp, type, count)
VALUES (@domain, @timestamp, @type, 1)
ON CONFLICT (domain, timestamp, type)
DO UPDATE SET count = aggregated_stats.count + 1;

-- name: QueryAggregatedStats :many
SELECT * FROM aggregated_stats
WHERE domain = @domain
AND timestamp >= @start AND timestamp < @stop
ORDER BY timestamp ASC, type;

-- name: DeleteAggregatedStatsOlderThan :execrows
DELETE FROM aggregated_stats WHERE timestamp < @before;

-- name: DeleteCountedStatEventsBefore :execrows
-- Compares the UTC second, the fixed-width prefix of stored_at, under the C
-- collation counted_stat_events_stored_second_idx is built with.
DELETE FROM counted_stat_events WHERE left(stored_at, 19) COLLATE "C" < @before_second::text;

-- name: CountAggregatedEvent :exec
WITH first_delivery AS (
    INSERT INTO counted_stat_events (stream, stored_at, sequence)
    VALUES (@stream, @stored_at, @sequence)
    ON CONFLICT DO NOTHING
    RETURNING 1
)
INSERT INTO aggregated_stats (domain, timestamp, type, count)
SELECT @domain, @timestamp, @type, 1 FROM first_delivery
ON CONFLICT (domain, timestamp, type)
DO UPDATE SET count = aggregated_stats.count + 1;
