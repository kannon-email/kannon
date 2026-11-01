package statssec

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// InMemKeyRepository is an in-memory KeyRepository, for the tests of everything above it and for
// one half of the repository specification.
type InMemKeyRepository struct {
	mu   sync.Mutex
	keys map[string]Key
}

// NewInMemKeyRepository returns an empty repository whose notion of "expired" follows the wall
// clock, as the sqlc-backed one follows the database's.
func NewInMemKeyRepository() *InMemKeyRepository {
	return &InMemKeyRepository{keys: map[string]Key{}}
}

func (r *InMemKeyRepository) SigningKey(_ context.Context, validUntil time.Time) (Key, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	for _, k := range r.keys {
		if k.ExpiresAt.After(validUntil) {
			return k, nil
		}
	}
	return Key{}, ErrNoSigningKey
}

func (r *InMemKeyRepository) Create(_ context.Context, k Key) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, ok := r.keys[k.ID]; ok {
		return fmt.Errorf("tracking key %q already exists", k.ID)
	}
	r.keys[k.ID] = k
	return nil
}

func (r *InMemKeyRepository) PublicKey(_ context.Context, id string) (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	k, ok := r.keys[id]
	if !ok || !k.ExpiresAt.After(time.Now()) {
		return "", ErrKeyNotFound
	}
	return k.PublicKey, nil
}

func (r *InMemKeyRepository) DeleteExpired(_ context.Context) (int64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	now := time.Now()
	var deleted int64
	for id, k := range r.keys {
		if k.ExpiresAt.Before(now) {
			delete(r.keys, id)
			deleted++
		}
	}
	return deleted, nil
}

// Len returns how many keys are stored, expired ones included.
func (r *InMemKeyRepository) Len() int {
	r.mu.Lock()
	defer r.mu.Unlock()

	return len(r.keys)
}
