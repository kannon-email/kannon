-- name: CreateTemplate :one
INSERT INTO templates (template_id, html, title, domain, type)
    VALUES ($1, $2, $3, $4, $5)
    RETURNING *;

-- name: UpdateTemplate :one
UPDATE templates SET
	html = $2,
	title = $3,
	updated_at = now()
WHERE template_id = $1
	RETURNING *;

-- name: DeleteTemplate :one
DELETE FROM templates WHERE template_id = $1
    RETURNING *;

-- name: GetTemplate :one
SELECT * FROM templates WHERE template_id = $1;

-- name: GetTemplates :many
SELECT * FROM templates WHERE domain = @domain AND type = 'template' LIMIT @take OFFSET @skip;

-- name: CountTemplates :one
SELECT COUNT(*) FROM templates WHERE domain = @domain AND type = 'template';