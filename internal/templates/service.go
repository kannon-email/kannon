package templates

import (
	"context"

	"github.com/kannon-email/kannon/internal/authz"
	"github.com/kannon-email/kannon/internal/values"
)

// Service is the seam every request-driven Template operation passes through,
// and therefore the one place each of them is authorized.
//
// The guards sit here rather than in the Connect handlers on purpose. A handler
// exists once per transport, so a requirement written there has to be restated
// for the next transport and can be forgotten for exactly one method; written
// here it is a property of the operation itself. It also keeps the transport
// layer's job to translation — parse the wire, render the response — which is
// what makes the three legacy adapters in pkg/api/adminapi readable as the
// workarounds they are.
//
// Every Domain is named by an values.DomainName, never a string. The Resource an
// authorization decision is made against embeds that value, so a spelling that
// never went through values.Parse would be a second name for one Domain and the
// whole path model rests on there being one (ADR 0008).
//
// The Mailer API deliberately does not come through here: it writes transient
// Templates as part of a send it has already authenticated by API Key, and
// wiring its Principal is the next slice. It holds the Repository directly.
type Service struct {
	repo Repository
}

// NewService creates a new Template service.
func NewService(repo Repository) *Service {
	return &Service{repo: repo}
}

// CreateTemplate authors a persistent Template for one Domain.
//
// The guard protects what a Domain's recipients read: a Template is the body of
// every mail sent with it, so authoring one for a Domain is speaking as that
// Domain. It is Create on the Domain's Templates collection rather than on the
// Template — the item does not exist yet, and its identifier is generated here
// rather than supplied.
func (s *Service) CreateTemplate(ctx context.Context, domain values.DomainName, html, title string) (*Template, error) {
	return authz.Guard(ctx, authz.Create, authz.Templates(domain), func() (*Template, error) {
		t, err := NewPersistent(domain, html, title)
		if err != nil {
			return nil, err
		}
		if err := s.repo.Create(ctx, t); err != nil {
			return nil, err
		}
		return t, nil
	})
}

// GetTemplates lists a Domain's persistent Templates, with the total that Domain
// holds.
//
// The guard is List and not Read because enumerating discloses something
// inspection does not: which Templates a Domain has is not the same as what is
// inside one, and ADR 0008 keeps the two Actions apart so that the first can be
// granted without the second.
//
// The result type is local to this function so that the signature stays the
// (values, total, error) shape the API Key service already uses: Guard carries
// one value, and inventing a package-level page type to satisfy it would put a
// name in the domain language that only a decorator needed.
func (s *Service) GetTemplates(ctx context.Context, domain values.DomainName, page Pagination) ([]*Template, int, error) {
	type listing struct {
		templates []*Template
		total     int
	}

	got, err := authz.Guard(ctx, authz.List, authz.Templates(domain), func() (listing, error) {
		found, err := s.repo.List(ctx, domain, page)
		if err != nil {
			return listing{}, err
		}
		total, err := s.repo.Count(ctx, domain)
		if err != nil {
			return listing{}, err
		}
		return listing{templates: found, total: total}, nil
	})

	return got.templates, got.total, err
}

// GetTemplate reads one Template of one Domain.
//
// The Domain is an explicit parameter rather than something recovered from the
// identifier here. Callers whose request carries no Domain recover it with
// DomainFromID and pass it in, which keeps the recovery visible at the one call
// site that needs it instead of hiding a parse inside every load.
//
// The load is domain-scoped, and that is what makes authorizing on a recovered
// Domain sound: the value the guard was given is the value the lookup uses, so a
// caller who names a Domain it does hold cannot reach a Template belonging to one
// it does not — the lookup simply finds nothing.
func (s *Service) GetTemplate(ctx context.Context, domain values.DomainName, templateID string) (*Template, error) {
	return authz.Guard(ctx, authz.Read, authz.Template(domain, templateID), func() (*Template, error) {
		return s.repo.FindByDomain(ctx, domain, templateID)
	})
}

// UpdateTemplate overwrites a Template's body and title.
//
// The domain-scoped load before the write is the point of it, not a cheap
// existence check: Repository.Update addresses a Template by identifier alone, so
// without this the authorization would be made against the Domain the caller
// named while the write landed on whatever row bore that id. FindByDomain first
// makes the two the same Template or makes the operation a not-found.
//
// There is no window between the two worth closing. A Template's Domain is part
// of its identifier and is never reassigned, so the fact the check establishes
// cannot become false while the write runs. A domain-scoped update in SQL would
// remove even that argument; it needs a new query and belongs to whoever next
// touches the schema.
func (s *Service) UpdateTemplate(ctx context.Context, domain values.DomainName, templateID, html, title string) (*Template, error) {
	return authz.Guard(ctx, authz.Update, authz.Template(domain, templateID), func() (*Template, error) {
		if _, err := s.repo.FindByDomain(ctx, domain, templateID); err != nil {
			return nil, err
		}
		return s.repo.Update(ctx, templateID, func(t *Template) error {
			t.SetHTML(html)
			t.SetTitle(title)
			return nil
		})
	})
}

// DeleteTemplate removes a Template and returns what was removed.
//
// Domain-scoped for the same reason UpdateTemplate is: Repository.Delete takes an
// identifier alone, and the guard's Resource must name the row the statement
// reaches.
func (s *Service) DeleteTemplate(ctx context.Context, domain values.DomainName, templateID string) (*Template, error) {
	return authz.Guard(ctx, authz.Delete, authz.Template(domain, templateID), func() (*Template, error) {
		if _, err := s.repo.FindByDomain(ctx, domain, templateID); err != nil {
			return nil, err
		}
		return s.repo.Delete(ctx, templateID)
	})
}
