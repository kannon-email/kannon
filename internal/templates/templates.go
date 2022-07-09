package templates

import (
	"context"
	"database/sql"

	sqlc "github.com/ludusrusso/kannon/internal/db"
)

// Manager implement interface to manage Templates
type Manager interface {
	FindTemplate(ctx context.Context, domain string, templateID string) (sqlc.Template, error)
	CreateTemplate(ctx context.Context, HTML string, domain string) (sqlc.Template, error)
}

// NewTemplateManager builds a Template Manager
func NewTemplateManager(db *sql.DB) (Manager, error) {
	return &manager{
		db: sqlc.New(db),
	}, nil
}
