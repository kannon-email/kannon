-- name: InsertStat :exec
INSERT INTO stats (email, message_id, type, timestamp, domain, data) VALUES  (@email, @message_id, @type, @timestamp, @domain, @data);

-- name: QueryStats :many
SELECT * FROM stats
WHERE domain = $1
AND timestamp >= @start AND timestamp < @stop
ORDER BY timestamp DESC
LIMIT @take OFFSET @skip;

-- name: CountQueryStats :one
SELECT COUNT(*) FROM stats
WHERE domain = $1
AND timestamp >= @start AND timestamp < @stop;

-- name: QueryStatsTimeline :many
SELECT
	type,
	COUNT(*) as count,
	date_trunc('hour', timestamp)::TIMESTAMP AS ts
FROM stats
WHERE domain = @domain
AND timestamp >= @start AND timestamp < @stop
GROUP BY type, ts
ORDER BY ts ASC, type;

-- name: DeleteStatsOlderThan :execrows
DELETE FROM stats WHERE timestamp < @before;