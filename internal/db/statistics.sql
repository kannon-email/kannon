-- name: InsertStat :exec
-- The unique index on (email, message_id, domain, type, timestamp) is what makes
-- this insert idempotent, and the timestamp is set by the publisher: a JetStream
-- redelivery of the same event carries the same one, so it collides with the row
-- the first delivery already wrote. DO NOTHING turns that second write into the
-- no-op it should be. Without it the insert raises a unique violation, the handler
-- Naks, and the event is redelivered until MaxDeliver gives up on it.
INSERT INTO stats (email, message_id, type, timestamp, domain, data) VALUES  (@email, @message_id, @type, @timestamp, @domain, @data)
ON CONFLICT (email, message_id, domain, type, timestamp) DO NOTHING;

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