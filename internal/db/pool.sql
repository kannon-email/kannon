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

-- name: SetSendingPoolDelivered :exec
UPDATE sending_pool_emails 
	SET status = 'sent' WHERE email = @email AND message_id = @message_id;

-- name: SetSendingPoolError :exec
UPDATE sending_pool_emails 
	SET status = 'error', error_msg = @error_msg 
    WHERE email = @email AND message_id = @message_id;