-- name: PrepareForSend :many
UPDATE sending_pool_emails AS sp
    SET status = 'sending'
    FROM (
            SELECT id FROM sending_pool_emails
            WHERE scheduled_time <= NOW() AND status = 'scheduled'
            LIMIT $1
        ) AS t
    WHERE sp.id = t.id
    RETURNING sp.*;

-- name: GetToVerify :many
SELECT * FROM sending_pool_emails
    WHERE status = 'to_verify' ORDER BY scheduled_time asc
    LIMIT $1;

-- name: SetSendingPoolDelivered :exec
UPDATE sending_pool_emails 
	SET status = 'sent' WHERE email = @email AND message_id = @message_id;


-- name: SetSendingPoolScheduled :exec
UPDATE sending_pool_emails 
	SET status = 'scheduled' WHERE email = @email AND message_id = @message_id;

-- name: CleanPool :exec
DELETE FROM sending_pool_emails 
WHERE email = @email AND message_id = @message_id;

-- name: ReschedulePool :exec
UPDATE sending_pool_emails 
SET status='scheduled', scheduled_time =  @scheduled_time, send_attempts_cnt = send_attempts_cnt + 1 WHERE email = @email AND message_id = @message_id;

-- name: GetPool :one
SELECT * FROM  sending_pool_emails 
WHERE email = @email AND message_id = @message_id;

-- name: GetSendingPoolsEmails :many
SELECT * FROM sending_pool_emails WHERE message_id = $1 LIMIT $2 OFFSET $3;;