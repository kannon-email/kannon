package apikeys

import (
	"context"
	"time"

	"github.com/kannon-email/kannon/internal/keyset"
	"github.com/kannon-email/kannon/internal/values"
)

type ListFilters struct {
	OnlyActive bool
}

// Pagination is the offset window legacy gRPC listings expose.
type Pagination struct {
	Limit  int
	Offset int
}

// Position is a key's place in the newest-first listing order: creation time, then ID, both descending.
type Position struct {
	CreatedAt time.Time
	ID        string
}

// PositionOf reports where k sits in the newest-first listing order.
func PositionOf(k *APIKey) Position {
	return Position{CreatedAt: k.CreatedAt(), ID: k.ID().String()}
}

// UpdateFunc is a function that modifies an API key
// Return an error to abort the transaction
type UpdateFunc func(key *APIKey) error

// Repository defines the interface for API key persistence operations. Every domain-scoped method
// names the Domain with a values.DomainName, so a lookup cannot be reached with a spelling that was
// never canonicalised — on an authentication path that would mean a valid key failing to resolve.
type Repository interface {
	Create(ctx context.Context, key *APIKey) error

	// FindByKeyHash resolves Bearer identity before a target Domain is authorized.
	FindByKeyHash(ctx context.Context, hash string) (*APIKey, error)

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

	// ListPage returns a Domain's keys newest first, strictly after page.After.
	ListPage(ctx context.Context, domain values.DomainName, page keyset.Page[Position]) ([]*APIKey, error)

	Count(ctx context.Context, domain values.DomainName, filters ListFilters) (int, error)
}
