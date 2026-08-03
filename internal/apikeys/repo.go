package apikeys

import (
	"context"

	"github.com/kannon-email/kannon/internal/values"
)

type ListFilters struct {
	OnlyActive bool
}

type Pagination struct {
	Limit  int
	Offset int
}

// UpdateFunc is a function that modifies an API key
// Return an error to abort the transaction
type UpdateFunc func(key *APIKey) error

// Repository defines the interface for API key persistence operations. Every domain-scoped method
// names the Domain with a values.DomainName, so a lookup cannot be reached with a spelling that was
// never canonicalised — on an authentication path that would mean a valid key failing to resolve.
type Repository interface {
	Create(ctx context.Context, key *APIKey) error

	// Update atomically reads, modifies, and persists a key within a transaction
	// The updateFn receives the current key and should modify it in place
	// Returns ErrKeyNotFound if the key doesn't exist for the domain
	Update(ctx context.Context, ref KeyRef, updateFn UpdateFunc) (*APIKey, error)

	// GetByKeyHash finds an API key by its SHA-256 hash for a specific domain
	// The caller is responsible for hashing the plaintext key before calling this method
	// Returns ErrKeyNotFound if the key doesn't exist
	GetByKeyHash(ctx context.Context, domain values.DomainName, keyHash string) (*APIKey, error)

	// GetByID finds an API key by its ID for a specific domain
	// Returns ErrKeyNotFound if the key doesn't exist for the domain
	GetByID(ctx context.Context, ref KeyRef) (*APIKey, error)

	List(ctx context.Context, domain values.DomainName, filters ListFilters, page Pagination) ([]*APIKey, error)

	Count(ctx context.Context, domain values.DomainName, filters ListFilters) (int, error)
}
