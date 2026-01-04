package apikeys

import (
	"context"
	"time"
)

type Service struct {
	repo Repository
}

// NewService creates a new API key service
func NewService(repo Repository) *Service {
	return &Service{repo: repo}
}

func (s *Service) CreateKey(ctx context.Context, domain, name string, expiresAt *time.Time) (*APIKey, error) {
	// Create key entity (validation happens in NewAPIKey)
	key, err := NewAPIKey(domain, name, expiresAt)
	if err != nil {
		return nil, err
	}

	// Persist to repository
	if err := s.repo.Create(ctx, key); err != nil {
		return nil, err
	}

	return key, nil
}

func (s *Service) GetKey(ctx context.Context, ref KeyRef) (*APIKey, error) {
	return s.repo.GetByID(ctx, ref)
}

func (s *Service) ListKeys(ctx context.Context, domain string, onlyActive bool, page Pagination) ([]*APIKey, error) {
	if err := validateDomain(domain); err != nil {
		return nil, err
	}

	filters := ListFilters{
		OnlyActive: onlyActive,
	}

	return s.repo.List(ctx, domain, filters, page)
}

func (s *Service) DeactivateKey(ctx context.Context, ref KeyRef) (*APIKey, error) {
	return s.repo.Update(ctx, ref, func(key *APIKey) error {
		key.Deactivate()
		return nil
	})
}

func (s *Service) ValidateForAuth(ctx context.Context, domain, key string) (*APIKey, error) {
	// Use optimized repository method that validates in database
	apiKey, err := s.repo.ValidateKeyForAuth(ctx, domain, key)
	if err != nil {
		// Always return generic error for security
		return nil, ErrKeyNotFound
	}

	return apiKey, nil
}
