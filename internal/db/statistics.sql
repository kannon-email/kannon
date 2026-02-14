-- name: InsertStat :exec
INSERT INTO stats (email, message_id, type, timestamp, domain, data) VALUES  (@email, @message_id, @type, @timestamp, @domain, @data);

-- name: QueryStats :many
SELECT * FROM stats 
WHERE domain = $1 
AND timestamp BETWEEN @start AND @stop
ORDER BY timestamp DESC
LIMIT @take OFFSET @skip;

-- name: CountQueryStats :one
SELECT COUNT(*) FROM stats 
WHERE domain = $1 
AND timestamp BETWEEN @start AND @stop;

-- name: QueryStatsTimeline :many
SELECT 
	type, 
	COUNT(*) as count, 
	date_trunc('hour', timestamp)::TIMESTAMP AS ts 
FROM stats 
WHERE domain = @domain
AND timestamp BETWEEN @start AND @stop
GROUP BY type, ts
ORDER BY ts DESC, type;

-- name: DeleteStatsOlderThan :execrows
DELETE FROM stats WHERE timestamp < @before;