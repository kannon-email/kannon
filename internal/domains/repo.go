package domains

import "context"

// Repository persists SenderDomain entities.
type Repository interface {
	// Create persists a new Domain. The DKIM key pair must already be
	// populated by New.
	Create(ctx context.Context, d *Domain) error

	// FindByName looks up a Domain by its FQDN.
	// Returns ErrDomainNotFound if not present.
	FindByName(ctx context.Context, fqdn string) (*Domain, error)

	// List returns all SenderDomains.
	List(ctx context.Context) ([]*Domain, error)
}
