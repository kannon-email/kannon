-- name: InsertDelivered :exec
INSERT INTO delivered (email, message_id, timestamp) VALUES ($1, $2, $3);

-- name: InsertAccepted :exec
INSERT INTO accepted (email, message_id) VALUES ($1, $2);

-- name: InsertBounced :exec
INSERT INTO bounced (email, message_id, err_code, err_msg, timestamp) VALUES ($1, $2, $3, $4, $5);