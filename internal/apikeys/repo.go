package apikeys

import (
	"context"
)

// ListFilters contains filters for listing API keys
type ListFilters struct {
	OnlyActive bool
}

// Pagination contains pagination parameters
type Pagination struct {
	Limit  int
	Offset int
}

// UpdateFunc is a function that modifies an API key
// Return an error to abort the transaction
type UpdateFunc func(key *APIKey) error

// Repository defines the interface for API key persistence operations
type Repository interface {
	// Create creates a new API key
	Create(ctx context.Context, key *APIKey) error

	// Update atomically reads, modifies, and persists a key within a transaction
	// The updateFn receives the current key and should modify it in place
	// Returns ErrKeyNotFound if the key doesn't exist for the domain
	Update(ctx context.Context, ref KeyRef, updateFn UpdateFunc) (*APIKey, error)

	// GetByKey finds an API key by its full key value for a specific domain
	// Returns ErrKeyNotFound if the key doesn't exist for the domain
	// Returns ErrInvalidKey if the key format is invalid
	GetByKey(ctx context.Context, domain, key string) (*APIKey, error)

	// ValidateKeyForAuth finds and validates an API key for authentication
	// This is an optimized query that checks active status and expiration in the database
	// Returns ErrKeyNotFound if the key doesn't exist, is inactive, or is expired
	// Returns ErrInvalidKey if the key format is invalid
	ValidateKeyForAuth(ctx context.Context, domain, key string) (*APIKey, error)

	// GetByID finds an API key by its ID for a specific domain
	// Returns ErrKeyNotFound if the key doesn't exist for the domain
	GetByID(ctx context.Context, ref KeyRef) (*APIKey, error)

	// List returns API keys for a domain with filters and pagination
	List(ctx context.Context, domain string, filters ListFilters, page Pagination) ([]*APIKey, error)
}
