package templates

import (
	"context"
	"crypto/rand"
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

func (m *manager) CreateTransientTemplate(ctx context.Context, HTML string, domain string) (sqlc.Template, error) {
	id, err := newTemplateID(domain)
	if err != nil {
		return sqlc.Template{}, err
	}
	template, err := m.db.CreateTemplate(ctx, sqlc.CreateTemplateParams{
		TemplateID: id,
		Html:       HTML,
		Domain:     domain,
		Title:      "",
		Type:       sqlc.TemplateTypeTransient,
	})
	if err != nil {
		return sqlc.Template{}, err
	}
	return template, nil
}

func (m *manager) CreateTemplate(ctx context.Context, HTML string, domain string, title string) (sqlc.Template, error) {
	id, err := newTemplateID(domain)
	if err != nil {
		return sqlc.Template{}, err
	}
	template, err := m.db.CreateTemplate(ctx, sqlc.CreateTemplateParams{
		TemplateID: id,
		Html:       HTML,
		Domain:     domain,
		Title:      title,
		Type:       sqlc.TemplateTypeTemplate,
	})
	if err != nil {
		return sqlc.Template{}, err
	}
	return template, nil
}

func (m *manager) UpdateTemplate(ctx context.Context, templateID string, HTML string, title string) (sqlc.Template, error) {
	template, err := m.db.UpdateTemplate(ctx, sqlc.UpdateTemplateParams{
		TemplateID: templateID,
		Html:       HTML,
		Title:      title,
	})
	if err != nil {
		return sqlc.Template{}, err
	}
	return template, nil
}

func (m *manager) DeleteTemplate(ctx context.Context, templateID string) (sqlc.Template, error) {
	template, err := m.db.DeleteTemplate(ctx, templateID)
	if err != nil {
		return sqlc.Template{}, err
	}
	return template, nil
}

func (m *manager) GetTemplate(ctx context.Context, templateID string) (sqlc.Template, error) {
	template, err := m.db.GetTemplate(ctx, templateID)
	if err != nil {
		return sqlc.Template{}, err
	}
	return template, nil
}

func (m *manager) GetTemplates(ctx context.Context, domain string, skip, take uint) ([]sqlc.Template, uint, error) {
	templates, err := m.db.GetTemplates(ctx, sqlc.GetTemplatesParams{
		Domain: domain,
		Skip:   int32(skip),
		Take:   int32(take),
	})
	if err != nil {
		return nil, 0, err
	}

	total, err := m.db.CountTemplates(ctx, domain)
	if err != nil {
		return nil, 0, err
	}

	return templates, uint(total), nil
}

func newTemplateID(domain string) (string, error) {
	id, err := cuid.NewCrypto(rand.Reader)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("template_%v@%v", id, domain), nil
}
