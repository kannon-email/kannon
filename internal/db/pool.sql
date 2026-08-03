-- name: PrepareForSend :many
UPDATE sending_pool_emails AS sp
    SET status = 'sending', claimed_at = NOW()
    FROM (
            SELECT id FROM sending_pool_emails
            WHERE scheduled_time <= NOW() AND status = 'scheduled'
            FOR UPDATE SKIP LOCKED
            LIMIT $1
        ) AS t
    WHERE sp.id = t.id
    RETURNING sp.*;

-- name: PrepareForValidate :many
UPDATE sending_pool_emails AS sp
    SET status = 'validating', claimed_at = NOW()
    FROM (
            SELECT id FROM sending_pool_emails
            WHERE status = 'to_validate'
            FOR UPDATE SKIP LOCKED
            LIMIT $1
        ) AS t
    WHERE sp.id = t.id
    RETURNING sp.*;

-- ReclaimStranded hands rows that have sat in an in-flight status past a
-- threshold back to the status they were claimed from, and clears the claim.
-- One query serves every in-flight status so the two cannot drift; the caller
-- supplies the status pair and whether the recovery counts as a send attempt,
-- and delivery.InFlight is what keeps those three from being paired wrongly.
--
-- The threshold is measured from claimed_at, never from scheduled_time: the
-- claim does not touch scheduled_time, so under a backlog that column is hours
-- in the past on a row claimed a second ago (ADR 0007).
--
-- name: ReclaimStranded :many
UPDATE sending_pool_emails AS sp
    SET status = @to_status,
        claimed_at = NULL,
        send_attempts_cnt = sp.send_attempts_cnt + CASE WHEN @bump_attempts::bool THEN 1 ELSE 0 END
    FROM (
            SELECT s.id FROM sending_pool_emails AS s
            WHERE s.status = @from_status
              AND s.claimed_at < NOW() - @stranded_for::interval
            FOR UPDATE SKIP LOCKED
            LIMIT @max
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
SELECT * FROM sending_pool_emails WHERE message_id = $1 ORDER BY id LIMIT $2 OFFSET $3;

-- name: CreateMessage :one
INSERT INTO messages
    (message_id, subject, sender_email, sender_alias, template_id, domain, attachments, headers, tracking) VALUES
    ($1, $2, $3, $4, $5, $6, $7, $8, $9) RETURNING *;

-- name: GetMessage :one
SELECT * FROM messages WHERE message_id = $1;

-- name: CreatePool :copyfrom
INSERT INTO sending_pool_emails (email, status, scheduled_time, original_scheduled_time, message_id, fields, domain, tracking) VALUES
    ($1, $2, $3, $4, $5, $6, $7, $8);

-- name: GetSendingData :one
SELECT
    t.html,
    m.domain,
    d.dkim_private_key,
    d.dkim_public_key,
    m.subject,
    m.message_id,
    m.sender_email,
    m.sender_alias,
    m.attachments,
    m.headers
FROM messages as m
    JOIN templates as t ON t.template_id = m.template_id
    JOIN domains as d ON d.domain = m.domain
    WHERE m.message_id = @message_id;