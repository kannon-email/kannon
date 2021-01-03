package templates

import (
	"gorm.io/gorm"
	"smtp.ludusrusso.space/internal/db"
)

type manager struct {
	db *gorm.DB
}

func (m *manager) FindTemplate(domain string, templateID string) (db.Template, error) {
	template := db.Template{
		TemplateID: templateID,
		Domain:     domain,
		Type:       db.TemplateTypePermanent,
	}
	err := m.db.Where(&template).First(&template).Error
	if err != nil {
		return db.Template{}, err
	}
	return template, nil
}

func (m *manager) CreateTmpTemplate(HTML string, domain string) (db.Template, error) {
	return m.createTemplate(HTML, domain, db.TemplateTypeTmp)
}

func (m *manager) CreatePermanentTemplate(HTML string, domain string) (db.Template, error) {
	return m.createTemplate(HTML, domain, db.TemplateTypePermanent)
}

func (m *manager) createTemplate(HTML string, domain string, templateType db.TemplateType) (db.Template, error) {
	template := db.Template{
		HTML:   HTML,
		Type:   templateType,
		Domain: domain,
	}

	if err := m.db.Create(&template).Error; err != nil {
		return db.Template{}, err
	}
	return template, nil
}
