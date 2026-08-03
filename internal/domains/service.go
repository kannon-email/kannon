package domains

import (
	"context"

	"github.com/kannon-email/kannon/internal/authz"
	"github.com/kannon-email/kannon/internal/tracking"
	"github.com/kannon-email/kannon/internal/values"
)

// Service is the seam every request-driven SenderDomain operation passes through, and therefore
// the one place each is authorized — a property of the operation rather than of one transport. The
// Mailer API stays out: its Domain read is part of authentication, and sender holds no read.
type Service struct {
	repo Repository
}

func NewService(repo Repository) *Service {
	return &Service{repo: repo}
}

// CreateDomain registers a new SenderDomain, generating its DKIM key pair. Create on the Domains
// collection — the shorter path, not a separate tier. This is the operation ADR 0008 opens with:
// unauthenticated, anyone reaching the listener can mint a Domain, a key and a valid credential.
func (s *Service) CreateDomain(ctx context.Context, name values.DomainName) (*Domain, error) {
	return authz.Guard(ctx, authz.Create, authz.Domains(), func() (*Domain, error) {
		d, err := New(name)
		if err != nil {
			return nil, err
		}
		if err := s.repo.Create(ctx, d); err != nil {
			return nil, err
		}
		return d, nil
	})
}

// GetDomains enumerates every SenderDomain. List on the Domains collection, which no Grant
// anchored on a single Domain reaches: a pattern longer than the Resource covers nothing, so an
// admin of example.com cannot learn which other Domains exist.
func (s *Service) GetDomains(ctx context.Context) ([]*Domain, error) {
	return authz.Guard(ctx, authz.List, authz.Domains(), func() ([]*Domain, error) {
		return s.repo.List(ctx)
	})
}

// GetDomain reads one SenderDomain, including its Tracking Policy and public
// DKIM key.
func (s *Service) GetDomain(ctx context.Context, name values.DomainName) (*Domain, error) {
	return authz.Guard(ctx, authz.Read, authz.Domain(name), func() (*Domain, error) {
		return s.repo.FindByName(ctx, name)
	})
}

// SetTrackingPolicy replaces the Domain's Tracking Policy — the ceiling every Batch and Recipient
// is resolved against. Update on the Domain, which by domination is also Update on its Templates;
// ADR 0008 accepts that, so the narrower Grant a reader might look for does not exist.
func (s *Service) SetTrackingPolicy(ctx context.Context, name values.DomainName, p tracking.Policy) (*Domain, error) {
	return authz.Guard(ctx, authz.Update, authz.Domain(name), func() (*Domain, error) {
		return s.repo.SetTrackingPolicy(ctx, name, p)
	})
}
