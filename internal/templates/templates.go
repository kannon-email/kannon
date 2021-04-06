package templates

import (
	"database/sql"

	"kannon.gyozatech.dev/generated/sqlc"
)

// Manager implement interface to manage Templates
type Manager interface {
	FindTemplate(domain string, templateID string) (sqlc.Template, error)
	CreateTemplate(HTML string, domain string) (sqlc.Template, error)
}

// NewTemplateManager builds a Template Manager
func NewTemplateManager(db *sql.DB) (Manager, error) {
	return &manager{
		db: sqlc.New(db),
	}, nil
}
