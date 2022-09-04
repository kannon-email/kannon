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

-- name: PrepareForValidate :many
UPDATE sending_pool_emails AS sp
    SET status = 'validating'
    FROM (
            SELECT id FROM sending_pool_emails
            WHERE status = 'to_validate'
            LIMIT $1
        ) AS t
    WHERE sp.id = t.id
    RETURNING sp.*;

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
SELECT * FROM sending_pool_emails WHERE message_id = $1 LIMIT $2 OFFSET $3;

-- name: CreateMessage :one
INSERT INTO messages
    (message_id, subject, sender_email, sender_alias, template_id, domain) VALUES
    ($1, $2, $3, $4, $5, $6) RETURNING *;

-- name: CreatePool :exec
INSERT INTO sending_pool_emails (email, status, scheduled_time, original_scheduled_time, message_id, fields, domain) VALUES 
    (@email, 'to_validate', @scheduled_time, @scheduled_time, @message_id, @fields, @domain);

-- name: GetSendingData :one
SELECT
    t.html,
    m.domain,
    d.dkim_private_key,
    d.dkim_public_key,
    m.subject,
    m.message_id,
    m.sender_email,
    m.sender_alias
FROM messages as m
    JOIN templates as t ON t.template_id = m.template_id
    JOIN domains as d ON d.domain = m.domain
    WHERE m.message_id = @message_id;