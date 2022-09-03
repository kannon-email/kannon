-- name: GetDomains :many
SELECT * FROM domains;


-- name: CreateMessage :one
INSERT INTO messages
    (message_id, subject, sender_email, sender_alias, template_id, domain) VALUES
    ($1, $2, $3, $4, $5, $6) RETURNING *;

-- name: CreatePoolWithFields :exec
INSERT INTO sending_pool_emails (email, status, scheduled_time, original_scheduled_time, message_id, fields) VALUES 
    (@email, 'to_verify', @scheduled_time, @scheduled_time, @message_id, @fields);

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

-- name: FindDomain :one
SELECT
    *
FROM domains
    WHERE domain = $1
;

-- name: GetAllDomains :many
SELECT
    *
FROM domains
;

-- name: FindDomainWithKey :one
SELECT * FROM domains
WHERE domain = $1
AND key = $2;

-- name: CreateDomain :one
INSERT INTO domains 
    (domain, key, dkim_private_key, dkim_public_key)
    VALUES ($1, $2, $3, $4) 
    RETURNING *;

-- name: FindTemplate :one
SELECT * FROM templates
WHERE template_id = $1
AND domain = $2;

-- name: SetDomainKey :one
UPDATE domains SET key = @key WHERE domain = @domain RETURNING *;