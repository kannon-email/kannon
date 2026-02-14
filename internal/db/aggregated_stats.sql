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
