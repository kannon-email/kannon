package apikeys

import (
	"context"
	"time"

	"github.com/kannon-email/kannon/internal/authz"
	"github.com/kannon-email/kannon/internal/values"
)

// Service is the seam every API Key operation passes through, and therefore the
// one place each of them is authorized.
//
// Every method here is guarded except ValidateForAuth, which is the method that
// *produces* the authority the others are checked against — see its own comment.
type Service struct {
	repo Repository
}

// NewService creates a new API key service
func NewService(repo Repository) *Service {
	return &Service{repo: repo}
}

// CreateKey mints a new API Key for a Domain and returns the plaintext once.
//
// The guard protects the ability to send as a Domain: a key is a credential, and
// minting one for example.com is granting whoever holds it the authority to send
// mail signed with that Domain's DKIM key.
func (s *Service) CreateKey(ctx context.Context, domain values.DomainName, name string, expiresAt *time.Time) (*CreateResult, error) {
	return authz.Guard(ctx, authz.Create, authz.APIKeys(domain), func() (*CreateResult, error) {
		// Create key entity (validation happens in NewAPIKey)
		result, err := NewAPIKey(domain, name, expiresAt)
		if err != nil {
			return nil, err
		}

		// Persist to repository
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

// ListKeys enumerates a Domain's API Keys, masked, with the total it holds.
//
// List rather than Read: knowing which credentials exist for a Domain, when they
// expire and which are still active is a different disclosure from inspecting one
// of them, and ADR 0008 keeps the two Actions apart so the second can be withheld.
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

// DeactivateKey revokes an API Key.
//
// The Action is Delete and not Update, and the choice is ADR 0008's rather than a
// reading of what the row does: deactivation is how a credential is removed from
// circulation, the key remains only so that what it signed stays attributable.
// Recording it as a change would fuse it with CreateKey under any coarser
// vocabulary, and on a credential system the distinction between minting and
// revoking is the one most worth having — neither a provisioner that cannot revoke
// nor an incident responder that cannot mint is expressible without it.
func (s *Service) DeactivateKey(ctx context.Context, ref KeyRef) (*APIKey, error) {
	return authz.Guard(ctx, authz.Delete, resourceOf(ref), func() (*APIKey, error) {
		return s.repo.Update(ctx, ref, func(key *APIKey) error {
			key.Deactivate()
			return nil
		})
	})
}

// ValidateForAuth resolves a plaintext key to the API Key it belongs to, or
// refuses.
//
// This one is deliberately *not* guarded, and the reason is not that it is
// harmless. It runs before anything has authenticated the request: it is the step
// that decides who the caller is, so requiring a Principal here would require the
// answer before the question. What protects it instead is that it discloses
// nothing on failure — every refusal is the same ErrKeyNotFound, so a caller
// cannot use it to learn which keys exist.
//
// Wrapping the key it returns in a Principal carrying a sender Grant on the key's
// own Domain belongs to the slice that authorizes the Mailer API.
func (s *Service) ValidateForAuth(ctx context.Context, domain values.DomainName, key string) (*APIKey, error) {
	// Hash the plaintext key before repo lookup
	keyHash := HashKey(key)

	// Get the key from repository
	apiKey, err := s.repo.GetByKeyHash(ctx, domain, keyHash)
	if err != nil {
		// Always return generic error for security (don't leak if key exists)
		return nil, ErrKeyNotFound
	}

	// Validate key is active
	if !apiKey.IsValid() {
		return nil, ErrKeyNotFound
	}

	return apiKey, nil
}

// resourceOf names the Resource a KeyRef points at.
//
// A KeyRef already carries the two things the path needs — a canonical FQDN and a
// validated identifier — so the Resource is assembled from them structurally and
// never from a joined string.
func resourceOf(ref KeyRef) authz.Resource {
	return authz.APIKey(ref.DomainName(), ref.KeyID().String())
}
