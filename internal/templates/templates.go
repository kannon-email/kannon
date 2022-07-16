package templates

import (
	"context"

	sqlc "github.com/ludusrusso/kannon/internal/db"
)

// Manager implement interface to manage Templates
type Manager interface {
	FindTemplate(ctx context.Context, domain string, templateID string) (sqlc.Template, error)
	CreateTemplate(ctx context.Context, HTML string, domain string) (sqlc.Template, error)
}

// NewTemplateManager builds a Template Manager
func NewTemplateManager(q *sqlc.Queries) Manager {
	return &manager{
		db: q,
	}
}
