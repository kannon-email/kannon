package templates

import (
	"context"

	"github.com/kannon-email/kannon/internal/authz"
	"github.com/kannon-email/kannon/internal/values"
)

// Service is the seam every request-driven Template operation passes through, and therefore the
// one place each is authorized — a property of the operation rather than of one transport. The
// Mailer API stays out: a send's transient Template is part of the Batch, not authored (ADR 0008).
type Service struct {
	repo Repository
}

func NewService(repo Repository) *Service {
	return &Service{repo: repo}
}

// CreateTemplate authors a persistent Template for one Domain. The guard protects what that
// Domain's recipients read: a Template is the body of every mail sent with it. Create on the
// collection rather than the item, since the identifier is generated here rather than supplied.
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

// GetTemplates lists a Domain's persistent Templates, with the total it holds. List and not Read,
// because enumerating discloses something inspection does not (ADR 0008). The result type is local
// so the signature keeps the (values, total, error) shape Guard's single value would break.
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

// GetTemplate reads one Template of one Domain. The Domain is an explicit parameter, recovered by
// the caller when its request carries none, so the recovery stays visible. The load is
// domain-scoped, which is what makes authorizing on a recovered Domain sound.
func (s *Service) GetTemplate(ctx context.Context, domain values.DomainName, templateID string) (*Template, error) {
	return authz.Guard(ctx, authz.Read, authz.Template(domain, templateID), func() (*Template, error) {
		return s.repo.FindByDomain(ctx, domain, templateID)
	})
}

// UpdateTemplate overwrites a Template's body and title. The domain-scoped load first is the
// point: Repository.Update addresses a Template by identifier alone, so without it the guard
// would check the Domain the caller named while the write landed on whatever row bore that id.
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

// DeleteTemplate removes a Template and returns what was removed. Domain-scoped for the same
// reason UpdateTemplate is: Repository.Delete takes an identifier alone, and the guard's Resource
// must name the row the statement reaches.
func (s *Service) DeleteTemplate(ctx context.Context, domain values.DomainName, templateID string) (*Template, error) {
	return authz.Guard(ctx, authz.Delete, authz.Template(domain, templateID), func() (*Template, error) {
		if _, err := s.repo.FindByDomain(ctx, domain, templateID); err != nil {
			return nil, err
		}
		return s.repo.Delete(ctx, templateID)
	})
}
