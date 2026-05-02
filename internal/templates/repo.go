package templates

import "context"

// Pagination contains pagination parameters for listing.
type Pagination struct {
	Skip uint
	Take uint
}

// UpdateFunc receives the current Template and may mutate it in place.
// Returning a non-nil error aborts the update.
type UpdateFunc func(t *Template) error

// Repository persists Template entities.
type Repository interface {
	// Create persists a new Template. The TemplateID must already be
	// populated by NewPersistent or NewTransient.
	Create(ctx context.Context, t *Template) error

	// Update atomically reads, modifies, and persists a Template.
	// Returns ErrTemplateNotFound if the template does not exist.
	Update(ctx context.Context, templateID string, fn UpdateFunc) (*Template, error)

	// Delete removes a Template by ID and returns the deleted row.
	// Returns ErrTemplateNotFound if not present.
	Delete(ctx context.Context, templateID string) (*Template, error)

	// GetByID looks up a Template by its ID alone.
	// Returns ErrTemplateNotFound if not present.
	GetByID(ctx context.Context, templateID string) (*Template, error)

	// FindByDomain looks up a Template by ID, scoped to a domain.
	// Returns ErrTemplateNotFound if not present.
	FindByDomain(ctx context.Context, domain, templateID string) (*Template, error)

	// List returns persistent templates for a domain with pagination.
	// Transient templates are excluded.
	List(ctx context.Context, domain string, page Pagination) ([]*Template, error)

	// Count returns the total number of persistent templates for a domain.
	Count(ctx context.Context, domain string) (int, error)
}
