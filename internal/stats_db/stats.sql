-- name: InsertPrepared :exec
INSERT INTO prepared (email, message_id, timestamp, domain) VALUES (@email, @message_id, @timestamp, @domain)
	ON CONFLICT (email, message_id, domain) DO UPDATE
	SET timestamp = @timestamp;

-- name: InsertAccepted :exec
INSERT INTO accepted (email, message_id, timestamp, domain) VALUES (@email, @message_id, @timestamp, @domain);

-- name: InsertHardBounced :exec
INSERT INTO hard_bounced (email, message_id, timestamp, domain, err_code, err_msg) VALUES  (@email, @message_id, @timestamp, @domain, @err_code, @err_msg);