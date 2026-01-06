package apikeyshelpers

import (
	"context"
	"sync"
	"time"

	"github.com/kannon-email/kannon/internal/apikeys"
)

// InMemoryRepository is a thread-safe in-memory implementation of apikeys.Repository
type InMemoryRepository struct {
	mu         sync.RWMutex
	byID       map[string]map[apikeys.ID]*apikeys.APIKey // domain -> id -> key
	byKeyValue map[string]map[string]*apikeys.APIKey     // domain -> keyValue -> key
}

// NewInMemoryRepository creates a new in-memory repository
func NewInMemoryRepository() *InMemoryRepository {
	return &InMemoryRepository{
		byID:       make(map[string]map[apikeys.ID]*apikeys.APIKey),
		byKeyValue: make(map[string]map[string]*apikeys.APIKey),
	}
}

// Create creates a new API key
func (r *InMemoryRepository) Create(ctx context.Context, key *apikeys.APIKey) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	domain := key.Domain()

	// Check if key already exists
	if domainKeys, exists := r.byKeyValue[domain]; exists {
		if _, exists := domainKeys[key.Key()]; exists {
			return apikeys.ErrKeyAlreadyExists
		}
	}

	// Initialize domain maps if needed
	if _, exists := r.byID[domain]; !exists {
		r.byID[domain] = make(map[apikeys.ID]*apikeys.APIKey)
	}
	if _, exists := r.byKeyValue[domain]; !exists {
		r.byKeyValue[domain] = make(map[string]*apikeys.APIKey)
	}

	// Store in both indexes
	r.byID[domain][key.ID()] = key
	r.byKeyValue[domain][key.Key()] = key

	return nil
}

// Update atomically reads, modifies, and persists a key within a transaction
func (r *InMemoryRepository) Update(ctx context.Context, ref apikeys.KeyRef, updateFn apikeys.UpdateFunc) (*apikeys.APIKey, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	domain := ref.Domain()
	keyID := ref.KeyID()

	// Find the key
	domainKeys, exists := r.byID[domain]
	if !exists {
		return nil, apikeys.ErrKeyNotFound
	}

	key, exists := domainKeys[keyID]
	if !exists {
		return nil, apikeys.ErrKeyNotFound
	}

	// Clone the key before applying update to prevent corruption on error
	keyClone := r.cloneKey(key)

	// Apply the update function to the clone
	if err := updateFn(keyClone); err != nil {
		return nil, err
	}

	// Success: update the stored key
	r.byID[domain][keyID] = keyClone
	r.byKeyValue[domain][key.Key()] = keyClone

	// Return a clone to prevent external mutation
	return r.cloneKey(keyClone), nil
}

// GetByKey finds an API key by its full key value for a specific domain
func (r *InMemoryRepository) GetByKey(ctx context.Context, domain, key string) (*apikeys.APIKey, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	domainKeys, exists := r.byKeyValue[domain]
	if !exists {
		return nil, apikeys.ErrKeyNotFound
	}

	apiKey, exists := domainKeys[key]
	if !exists {
		return nil, apikeys.ErrKeyNotFound
	}

	return r.cloneKey(apiKey), nil
}

// GetByID finds an API key by its ID for a specific domain
func (r *InMemoryRepository) GetByID(ctx context.Context, ref apikeys.KeyRef) (*apikeys.APIKey, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	domain := ref.Domain()
	keyID := ref.KeyID()

	domainKeys, exists := r.byID[domain]
	if !exists {
		return nil, apikeys.ErrKeyNotFound
	}

	apiKey, exists := domainKeys[keyID]
	if !exists {
		return nil, apikeys.ErrKeyNotFound
	}

	return r.cloneKey(apiKey), nil
}

// List returns API keys for a domain with filters and pagination
func (r *InMemoryRepository) List(ctx context.Context, domain string, filters apikeys.ListFilters, page apikeys.Pagination) ([]*apikeys.APIKey, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	domainKeys, exists := r.byID[domain]
	if !exists {
		return []*apikeys.APIKey{}, nil
	}

	// Collect all keys for the domain
	allKeys := make([]*apikeys.APIKey, 0, len(domainKeys))
	for _, key := range domainKeys {
		// Apply active filter
		if filters.OnlyActive && !key.IsActiveStatus() {
			continue
		}
		allKeys = append(allKeys, key)
	}

	// Apply pagination
	start := page.Offset
	if start > len(allKeys) {
		return []*apikeys.APIKey{}, nil
	}

	end := start + page.Limit
	if end > len(allKeys) {
		end = len(allKeys)
	}

	result := make([]*apikeys.APIKey, 0, end-start)
	for i := start; i < end; i++ {
		result = append(result, r.cloneKey(allKeys[i]))
	}

	return result, nil
}

// Count returns the total number of API keys for a domain with filters
func (r *InMemoryRepository) Count(ctx context.Context, domain string, filters apikeys.ListFilters) (int, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	domainKeys, exists := r.byID[domain]
	if !exists {
		return 0, nil
	}

	count := 0
	for _, key := range domainKeys {
		// Apply active filter
		if filters.OnlyActive && !key.IsActiveStatus() {
			continue
		}
		count++
	}

	return count, nil
}

// cloneKey creates a copy of an API key to prevent external mutation
func (r *InMemoryRepository) cloneKey(key *apikeys.APIKey) *apikeys.APIKey {
	return apikeys.LoadAPIKey(apikeys.LoadAPIKeyParams{
		ID:            key.ID(),
		Key:           key.Key(),
		Name:          key.Name(),
		Domain:        key.Domain(),
		CreatedAt:     key.CreatedAt(),
		ExpiresAt:     r.cloneTimePtr(key.ExpiresAt()),
		IsActive:      key.IsActiveStatus(),
		DeactivatedAt: r.cloneTimePtr(key.DeactivatedAt()),
	})
}

// cloneTimePtr clones a *time.Time
func (r *InMemoryRepository) cloneTimePtr(t *time.Time) *time.Time {
	if t == nil {
		return nil
	}
	cloned := *t
	return &cloned
}
