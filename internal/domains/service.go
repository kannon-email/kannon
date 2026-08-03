package domains

import (
	"context"

	"github.com/kannon-email/kannon/internal/authz"
	"github.com/kannon-email/kannon/internal/tracking"
	"github.com/kannon-email/kannon/internal/values"
)

// Service is the seam every request-driven SenderDomain operation passes
// through, and therefore the one place each of them is authorized.
//
// The guards sit here and not in the Connect handlers so that the requirement is
// a property of the operation rather than of one transport: a second transport
// over the same domain would otherwise need the whole map restated, and a
// restatement can be one method short. What is left in the handler is
// translation — parse the wire, render the response.
//
// The Mailer API deliberately does not come through here. It resolves the calling
// Domain from an API Key it has already authenticated, and its Principal is the
// next slice; it holds the Repository directly.
type Service struct {
	repo Repository
}

// NewService creates a new SenderDomain service.
func NewService(repo Repository) *Service {
	return &Service{repo: repo}
}

// CreateDomain registers a new SenderDomain, generating its DKIM key pair.
//
// The guard is Create on the Domains collection — the shorter path, not a
// separate tier of authority. This is the operation ADR 0008 opens with: while it
// sits on an unauthenticated surface, anyone who can reach the listener creates a
// Domain, mints a key for it and sends with a perfectly valid credential, which
// is what makes the authentication the Mailer API does perform protect nothing.
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

// GetDomains enumerates every SenderDomain.
//
// List on the Domains collection, which no Grant anchored on a single Domain
// reaches: a pattern longer than the Resource covers nothing, so an admin of
// example.com cannot learn which other Domains exist. That falls out of prefix
// domination rather than being checked here.
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

// SetTrackingPolicy replaces the Domain's Tracking Policy — the ceiling every
// Batch and Recipient of this Domain is resolved against.
//
// Update on the Domain itself, which by prefix domination is also Update on that
// Domain's Templates. ADR 0008 accepts that rather than working around it:
// changing a Tracking Policy and rewriting Templates are both things a Domain
// administrator does, and the alternative — a domains/<fqdn>/tracking path
// corresponding to no entity in the language — buys a Role nobody has asked for.
// So this authority is not separable from Template authorship, and a reader
// looking for the narrower Grant should know it does not exist.
func (s *Service) SetTrackingPolicy(ctx context.Context, name values.DomainName, p tracking.Policy) (*Domain, error) {
	return authz.Guard(ctx, authz.Update, authz.Domain(name), func() (*Domain, error) {
		return s.repo.SetTrackingPolicy(ctx, name, p)
	})
}
