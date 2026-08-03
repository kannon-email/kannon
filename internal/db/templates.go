package sqlc

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/kannon-email/kannon/internal/templates"
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
		Domain:     t.Domain(),
		Type:       toSQLCTemplateType(t.Type()),
	})
	if err != nil {
		return err
	}
	*t = *rowToTemplate(row)
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
	return rowToTemplate(row), nil
}

func (r *templatesRepository) Delete(ctx context.Context, templateID string) (*templates.Template, error) {
	q := New(r.db)
	row, err := q.DeleteTemplate(ctx, templateID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, templates.ErrTemplateNotFound
		}
		// The only key pointing at a Template is the one messages holds, so a
		// refusal here is a Batch still referencing it (ADR 0008).
		if isForeignKeyViolation(err) {
			return nil, templates.ErrTemplateInUse
		}
		return nil, err
	}
	return rowToTemplate(row), nil
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
	return rowToTemplate(row), nil
}

func (r *templatesRepository) FindByDomain(ctx context.Context, domain, templateID string) (*templates.Template, error) {
	q := New(r.db)
	row, err := q.FindTemplate(ctx, FindTemplateParams{
		TemplateID: templateID,
		Domain:     domain,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, templates.ErrTemplateNotFound
		}
		return nil, err
	}
	return rowToTemplate(row), nil
}

func (r *templatesRepository) List(ctx context.Context, domain string, page templates.Pagination) ([]*templates.Template, error) {
	q := New(r.db)
	rows, err := q.GetTemplates(ctx, GetTemplatesParams{
		Domain: domain,
		Skip:   int32(page.Skip),
		Take:   int32(page.Take),
	})
	if err != nil {
		return nil, err
	}
	out := make([]*templates.Template, 0, len(rows))
	for _, row := range rows {
		out = append(out, rowToTemplate(row))
	}
	return out, nil
}

func (r *templatesRepository) Count(ctx context.Context, domain string) (int, error) {
	q := New(r.db)
	n, err := q.CountTemplates(ctx, domain)
	if err != nil {
		return 0, err
	}
	return int(n), nil
}

func rowToTemplate(row Template) *templates.Template {
	return templates.Load(templates.LoadParams{
		TemplateID: row.TemplateID,
		Html:       row.Html,
		Title:      row.Title,
		Domain:     row.Domain,
		Type:       fromSQLCTemplateType(row.Type),
		CreatedAt:  row.CreatedAt.Time,
		UpdatedAt:  row.UpdatedAt.Time,
	})
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
