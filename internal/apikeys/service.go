package apikeys

import (
	"context"
	"time"

	"github.com/kannon-email/kannon/internal/authz"
	"github.com/kannon-email/kannon/internal/values"
)

// Service is the seam every API Key operation passes through, and therefore the one place each of
// them is authorized. Every method here is guarded except ValidateForAuth, which produces the
// authority the others are checked against — see its own comment.
type Service struct {
	repo Repository
}

func NewService(repo Repository) *Service {
	return &Service{repo: repo}
}

// CreateKey mints a new API Key for a Domain and returns the plaintext once. The guard protects
// the ability to send as a Domain: minting a key for example.com grants whoever holds it the
// authority to send mail signed with that Domain's DKIM key.
func (s *Service) CreateKey(ctx context.Context, domain values.DomainName, name string, expiresAt *time.Time) (*CreateResult, error) {
	return authz.Guard(ctx, authz.Create, authz.APIKeys(domain), func() (*CreateResult, error) {
		result, err := NewAPIKey(domain, name, expiresAt)
		if err != nil {
			return nil, err
		}

		if err := s.repo.Create(ctx, result.Key); err != nil {
			return nil, err
		}

		return result, nil
	})
}

// GetKey reads one API Key of one Domain, masked.
func (s *Service) GetKey(ctx context.Context, ref KeyRef) (*APIKey, error) {
	return authz.Guard(ctx, authz.Read, resourceOf(ref), func() (*APIKey, error) {
		return s.repo.GetByID(ctx, ref)
	})
}

// ListKeys enumerates a Domain's API Keys, masked, with the total it holds. List rather than Read:
// knowing which credentials exist, when they expire and which are active is a different disclosure
// from inspecting one, and ADR 0008 keeps the two apart so the second can be withheld.
func (s *Service) ListKeys(ctx context.Context, domain values.DomainName, onlyActive bool, page Pagination) ([]*APIKey, int, error) {
	type listing struct {
		keys  []*APIKey
		total int
	}

	got, err := authz.Guard(ctx, authz.List, authz.APIKeys(domain), func() (listing, error) {
		filters := ListFilters{
			OnlyActive: onlyActive,
		}

		keys, err := s.repo.List(ctx, domain, filters, page)
		if err != nil {
			return listing{}, err
		}

		total, err := s.repo.Count(ctx, domain, filters)
		if err != nil {
			return listing{}, err
		}

		return listing{keys: keys, total: total}, nil
	})

	return got.keys, got.total, err
}

// DeactivateKey revokes an API Key. The Action is Delete and not Update, per ADR 0008: revocation
// is a removal from circulation, and on a credential system the distinction between minting and
// revoking is the one most worth having — a coarser vocabulary would fuse it with CreateKey.
func (s *Service) DeactivateKey(ctx context.Context, ref KeyRef) (*APIKey, error) {
	return authz.Guard(ctx, authz.Delete, resourceOf(ref), func() (*APIKey, error) {
		return s.repo.Update(ctx, ref, func(key *APIKey) error {
			key.Deactivate()
			return nil
		})
	})
}

// ValidateForAuth resolves a plaintext key to the API Key it belongs to, or refuses. Deliberately
// unguarded: it decides who the caller is, so requiring a Principal would require the answer before
// the question. What protects it is that every refusal is the same ErrKeyNotFound.
func (s *Service) ValidateForAuth(ctx context.Context, domain values.DomainName, key string) (*APIKey, error) {
	keyHash := HashKey(key)

	apiKey, err := s.repo.GetByKeyHash(ctx, domain, keyHash)
	if err != nil {
		// Always return generic error for security (don't leak if key exists)
		return nil, ErrKeyNotFound
	}

	if !apiKey.IsValid() {
		return nil, ErrKeyNotFound
	}

	return apiKey, nil
}

// resourceOf names the Resource a KeyRef points at. A KeyRef already carries the two things the
// path needs — a canonical domain name and a validated identifier — so the Resource is assembled
// from them structurally and never from a joined string.
func resourceOf(ref KeyRef) authz.Resource {
	return authz.APIKey(ref.DomainName(), ref.KeyID().String())
}
