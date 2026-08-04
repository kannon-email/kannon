package domains

import (
	"context"

	"github.com/kannon-email/kannon/internal/tracking"
	"github.com/kannon-email/kannon/internal/values"
)

// Repository persists SenderDomain entities. Every method that names a Domain names it with a
// values.DomainName, so a query cannot be reached with a name that was never canonicalised: two
// spellings of one mail domain would otherwise be two Domains, each with its own DKIM keypair.
type Repository interface {
	// Create persists a new Domain. The DKIM key pair must already be
	// populated by New.
	Create(ctx context.Context, d *Domain) error

	// SetTrackingPolicy replaces the Domain's Tracking Policy and returns the updated Domain. A
	// Mode that states nothing is normalised to off before it is persisted, so the empty string
	// never appears at rest. Returns ErrDomainNotFound if not present.
	SetTrackingPolicy(ctx context.Context, domain values.DomainName, p tracking.Policy) (*Domain, error)

	// FindByName looks up a Domain by its domain name.
	// Returns ErrDomainNotFound if not present.
	FindByName(ctx context.Context, domain values.DomainName) (*Domain, error)

	// List returns all SenderDomains.
	List(ctx context.Context) ([]*Domain, error)
}
