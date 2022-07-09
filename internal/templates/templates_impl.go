package templates

import (
	"context"
	"fmt"

	"github.com/lucsky/cuid"
	sqlc "github.com/ludusrusso/kannon/internal/db"
)

type manager struct {
	db *sqlc.Queries
}

func (m *manager) FindTemplate(ctx context.Context, domain string, templateID string) (sqlc.Template, error) {
	template, err := m.db.FindTemplate(ctx, sqlc.FindTemplateParams{
		TemplateID: templateID,
		Domain:     domain,
	})
	if err != nil {
		return sqlc.Template{}, err
	}
	return template, nil
}

func (m *manager) CreateTemplate(ctx context.Context, html string, domain string) (sqlc.Template, error) {
	template, err := m.db.CreateTemplate(ctx, sqlc.CreateTemplateParams{
		TemplateID: fmt.Sprintf("template_%v@%v", cuid.New(), domain),
		Html:       html,
		Domain:     domain,
	})
	if err != nil {
		return sqlc.Template{}, err
	}
	return template, nil
}
