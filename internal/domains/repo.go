package domains

import (
	"context"

	"github.com/kannon-email/kannon/internal/tracking"
)

// Repository persists SenderDomain entities.
type Repository interface {
	// Create persists a new Domain. The DKIM key pair must already be
	// populated by New.
	Create(ctx context.Context, d *Domain) error

	// SetTrackingPolicy replaces the Domain's Tracking Policy and returns the
	// updated Domain. A Mode that states nothing is normalised to off before it
	// is persisted, so the empty string never appears at rest on a Domain.
	// Returns ErrDomainNotFound if not present.
	SetTrackingPolicy(ctx context.Context, fqdn string, p tracking.Policy) (*Domain, error)

	// FindByName looks up a Domain by its FQDN.
	// Returns ErrDomainNotFound if not present.
	FindByName(ctx context.Context, fqdn string) (*Domain, error)

	// List returns all SenderDomains.
	List(ctx context.Context) ([]*Domain, error)
}
