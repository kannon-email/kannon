-- name: InsertPrepared :exec
INSERT INTO prepared (email, message_id, timestamp, first_timestamp, domain) VALUES (@email, @message_id, @timestamp, @timestamp, @domain)
	ON CONFLICT (email, message_id, domain) DO UPDATE
	SET timestamp = @timestamp;

-- name: InsertStat :exec
INSERT INTO stats (email, message_id, type, timestamp, domain, data) VALUES  (@email, @message_id, @type, @timestamp, @domain, @data);