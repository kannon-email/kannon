package templates

import (
	"context"
	"fmt"

	"github.com/lucsky/cuid"
	"kannon.gyozatech.dev/generated/sqlc"
)

type manager struct {
	db *sqlc.Queries
}

func (m *manager) FindTemplate(domain string, templateID string) (sqlc.Template, error) {
	template, err := m.db.FindTemplate(context.TODO(), sqlc.FindTemplateParams{
		TemplateID: templateID,
		Domain:     domain,
	})
	if err != nil {
		return sqlc.Template{}, err
	}
	return template, nil
}

func (m *manager) CreateTemplate(HTML string, domain string) (sqlc.Template, error) {
	template, err := m.db.CreateTemplate(context.TODO(), sqlc.CreateTemplateParams{
		TemplateID: fmt.Sprintf("template_%v@%v", cuid.New(), domain),
		Html:       HTML,
		Domain:     domain,
	})
	if err != nil {
		return sqlc.Template{}, err
	}
	return template, nil
}
