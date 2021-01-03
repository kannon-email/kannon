package templates

import (
	"gorm.io/gorm"
	"smtp.ludusrusso.space/internal/db"
)

// Manager implement interface to manage Templates
type Manager interface {
	FindTemplate(domain string, templateID string) (db.Template, error)
	CreatePermanentTemplate(HTML string, domain string) (db.Template, error)
	CreateTmpTemplate(HTML string, domain string) (db.Template, error)
}

// NewTemplateManager builds a Template Manager
func NewTemplateManager(db *gorm.DB) (Manager, error) {
	return &manager{
		db: db,
	}, nil
}
