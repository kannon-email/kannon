package sqlc

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/kannon-email/kannon/internal/templates"
	"github.com/kannon-email/kannon/internal/values"
)

type templatesRepository struct {
	db *pgxpool.Pool
}

// NewTemplatesRepository creates a new PostgreSQL-backed Template repository.
func NewTemplatesRepository(db *pgxpool.Pool) templates.Repository {
	return &templatesRepository{db: db}
}

func (r *templatesRepository) Create(ctx context.Context, t *templates.Template) error {
	q := New(r.db)
	row, err := q.CreateTemplate(ctx, CreateTemplateParams{
		TemplateID: t.TemplateID(),
		Html:       t.Html(),
		Title:      t.Title(),
		Domain:     t.DomainName().String(),
		Type:       toSQLCTemplateType(t.Type()),
	})
	if err != nil {
		return err
	}
	loaded, err := rowToTemplate(row)
	if err != nil {
		return err
	}
	*t = *loaded
	return nil
}

func (r *templatesRepository) Update(ctx context.Context, templateID string, fn templates.UpdateFunc) (*templates.Template, error) {
	current, err := r.GetByID(ctx, templateID)
	if err != nil {
		return nil, err
	}
	if err := fn(current); err != nil {
		return nil, err
	}
	q := New(r.db)
	row, err := q.UpdateTemplate(ctx, UpdateTemplateParams{
		TemplateID: current.TemplateID(),
		Html:       current.Html(),
		Title:      current.Title(),
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, templates.ErrTemplateNotFound
		}
		return nil, err
	}
	return rowToTemplate(row)
}

func (r *templatesRepository) Delete(ctx context.Context, templateID string) (*templates.Template, error) {
	q := New(r.db)
	row, err := q.DeleteTemplate(ctx, templateID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, templates.ErrTemplateNotFound
		}
		return nil, err
	}
	return rowToTemplate(row)
}

func (r *templatesRepository) GetByID(ctx context.Context, templateID string) (*templates.Template, error) {
	q := New(r.db)
	row, err := q.GetTemplate(ctx, templateID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, templates.ErrTemplateNotFound
		}
		return nil, err
	}
	return rowToTemplate(row)
}

func (r *templatesRepository) FindByDomain(ctx context.Context, domain values.DomainName, templateID string) (*templates.Template, error) {
	q := New(r.db)
	row, err := q.FindTemplate(ctx, FindTemplateParams{
		TemplateID: templateID,
		Domain:     domain.String(),
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, templates.ErrTemplateNotFound
		}
		return nil, err
	}
	return rowToTemplate(row)
}

func (r *templatesRepository) List(ctx context.Context, domain values.DomainName, page templates.Pagination) ([]*templates.Template, error) {
	q := New(r.db)
	rows, err := q.GetTemplates(ctx, GetTemplatesParams{
		Domain: domain.String(),
		Skip:   int32(page.Skip),
		Take:   int32(page.Take),
	})
	if err != nil {
		return nil, err
	}
	out := make([]*templates.Template, 0, len(rows))
	for _, row := range rows {
		tpl, err := rowToTemplate(row)
		if err != nil {
			return nil, err
		}
		out = append(out, tpl)
	}
	return out, nil
}

func (r *templatesRepository) Count(ctx context.Context, domain values.DomainName) (int, error) {
	q := New(r.db)
	n, err := q.CountTemplates(ctx, domain.String())
	if err != nil {
		return 0, err
	}
	return int(n), nil
}

// rowToTemplate rebuilds the entity from its row, canonicalising the stored domain name for the same
// reason rowToDomain does: a Template whose Domain cannot be parsed is one no domain-scoped lookup
// could return, and saying so beats handing back an entity addressed to nothing.
func rowToTemplate(row Template) (*templates.Template, error) {
	domain, err := values.Parse(row.Domain)
	if err != nil {
		return nil, fmt.Errorf("template row %q holds a non-canonical domain %q: %w", row.TemplateID, row.Domain, err)
	}
	return templates.Load(templates.LoadParams{
		TemplateID: row.TemplateID,
		Html:       row.Html,
		Title:      row.Title,
		Domain:     domain,
		Type:       fromSQLCTemplateType(row.Type),
		CreatedAt:  row.CreatedAt.Time,
		UpdatedAt:  row.UpdatedAt.Time,
	}), nil
}

func toSQLCTemplateType(t templates.Type) TemplateType {
	switch t {
	case templates.TypeTransient:
		return TemplateTypeTransient
	case templates.TypePersistent:
		return TemplateTypeTemplate
	default:
		return TemplateTypeTransient
	}
}

func fromSQLCTemplateType(t TemplateType) templates.Type {
	switch t {
	case TemplateTypeTransient:
		return templates.TypeTransient
	case TemplateTypeTemplate:
		return templates.TypePersistent
	default:
		return templates.TypeTransient
	}
}
