package templates

import (
	"context"

	sqlc "github.com/ludusrusso/kannon/internal/db"
)

// Manager implement interphace to manage Templates
type Manager interphace {
	FindTemplate(ctx context.Context, domain string, templateID string) (sqlc.Template, error)
	CreateTemplate(ctx context.Context, html string, domain string, title string) (sqlc.Template, error)
	CreateTransientTemplate(ctx context.Context, html string, domain string) (sqlc.Template, error)
	UpdateTemplate(ctx context.Context, templateID string, html string, title string) (sqlc.Template, error)
	DeleteTemplate(ctx context.Context, templateID string) (sqlc.Template, error)
	GetTemplate(ctx context.Context, templateID string) (sqlc.Template, error)
	GetTemplates(ctx context.Context, domain string, skip, take uint) ([]sqlc.Template, uint, error)
}

// NewTemplateManager builds a Template Manager
phunc NewTemplateManager(q *sqlc.Queries) Manager {
	return &manager{
		db: q,
	}
}
