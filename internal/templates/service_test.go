package templates_test

import (
	"context"
	"testing"

	"github.com/kannon-email/kannon/internal/authz"
	"github.com/kannon-email/kannon/internal/keyset"
	"github.com/kannon-email/kannon/internal/templates"
	"github.com/kannon-email/kannon/internal/values"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Fixture Domains and a Template belonging to the first of them. MustParse is right
// for package-level values in a test: a bad one is a bug in this file, not input.
var (
	homeDomain  = values.MustParse("example.com")
	otherDomain = values.MustParse("other.com")
)

// seededID is spelled out rather than generated so that the tests below can name the
// Template they mean. It is in the format newTemplateID composes, because
// DomainFromID has to be able to take it apart.
const seededID = "template_seed@example.com"

// Principals, one Grant each, named for the authority they hold rather than for the test that uses
// them. otherDomainAdmin is the case worth having: exactly as much power as homeDomainAdmin and
// none of it here, which keeps the table honest about reach being the Anchor's business.
var (
	rootAdmin        = authz.MustNewPrincipal("root-admin", authz.MustNewGrant(authz.RoleAdmin, authz.RootAnchor()))
	everyDomainAdmin = authz.MustNewPrincipal("every-domain-admin", authz.MustNewGrant(authz.RoleAdmin, authz.AllDomainsAnchor()))
	homeDomainAdmin  = authz.MustNewPrincipal("home-domain-admin", authz.MustNewGrant(authz.RoleAdmin, authz.DomainAnchor(homeDomain)))
	otherDomainAdmin = authz.MustNewPrincipal("other-domain-admin", authz.MustNewGrant(authz.RoleAdmin, authz.DomainAnchor(otherDomain)))
	senderOnly       = authz.MustNewPrincipal("sender-only", authz.MustNewGrant(authz.RoleSender, authz.DomainAnchor(homeDomain)))
	noGrants         = authz.MustNewPrincipal("no-grants")
)

// TestServiceAuthorization is the table that says what each operation demands. Two things are
// asserted for every refusal: the error is ErrForbidden, and the repository was never reached — a
// guard that ran the operation and then refused would satisfy the first. sender is refused by all.
func TestServiceAuthorization(t *testing.T) {
	ops := []struct {
		name  string
		call  func(context.Context, *templates.Service) error
		allow []authz.Principal
		deny  []authz.Principal
	}{
		{
			name: "CreateTemplate",
			call: func(ctx context.Context, s *templates.Service) error {
				_, err := s.CreateTemplate(ctx, homeDomain, "<p>hi</p>", "hi")
				return err
			},
			allow: []authz.Principal{rootAdmin, everyDomainAdmin, homeDomainAdmin},
			deny:  []authz.Principal{otherDomainAdmin, senderOnly, noGrants},
		},
		{
			name: "GetTemplates",
			call: func(ctx context.Context, s *templates.Service) error {
				_, _, err := s.GetTemplates(ctx, homeDomain, templates.Pagination{Take: 10})
				return err
			},
			allow: []authz.Principal{rootAdmin, everyDomainAdmin, homeDomainAdmin},
			deny:  []authz.Principal{otherDomainAdmin, senderOnly, noGrants},
		},
		{
			name: "GetTemplate",
			call: func(ctx context.Context, s *templates.Service) error {
				_, err := s.GetTemplate(ctx, homeDomain, seededID)
				return err
			},
			allow: []authz.Principal{rootAdmin, everyDomainAdmin, homeDomainAdmin},
			deny:  []authz.Principal{otherDomainAdmin, senderOnly, noGrants},
		},
		{
			name: "UpdateTemplate",
			call: func(ctx context.Context, s *templates.Service) error {
				_, err := s.UpdateTemplate(ctx, homeDomain, seededID, new("<p>new</p>"), new("new"))
				return err
			},
			allow: []authz.Principal{rootAdmin, everyDomainAdmin, homeDomainAdmin},
			deny:  []authz.Principal{otherDomainAdmin, senderOnly, noGrants},
		},
		{
			name: "DeleteTemplate",
			call: func(ctx context.Context, s *templates.Service) error {
				_, err := s.DeleteTemplate(ctx, homeDomain, seededID)
				return err
			},
			allow: []authz.Principal{rootAdmin, everyDomainAdmin, homeDomainAdmin},
			deny:  []authz.Principal{otherDomainAdmin, senderOnly, noGrants},
		},
	}

	for _, op := range ops {
		t.Run(op.name, func(t *testing.T) {
			for _, p := range op.allow {
				t.Run("proceeds for "+p.ID(), func(t *testing.T) {
					repo := seededRepo()
					service := templates.NewService(repo)

					err := op.call(authz.NewContext(t.Context(), p), service)
					require.NoError(t, err)
					assert.Positive(t, repo.reached, "the operation was authorized but never ran")
				})
			}

			for _, p := range op.deny {
				t.Run("refuses "+p.ID(), func(t *testing.T) {
					repo := seededRepo()
					service := templates.NewService(repo)

					err := op.call(authz.NewContext(t.Context(), p), service)
					assert.ErrorIs(t, err, authz.ErrForbidden)
					assert.Zero(t, repo.reached, "a refused operation must not reach the repository")
				})
			}

			// Nothing authenticated the request at all, which is what the API's interceptor
			// leaves behind when it refuses a token. It has to be distinguishable from a
			// Principal that may not do this, because the two are very different problems.
			t.Run("refuses a request with no Principal", func(t *testing.T) {
				repo := seededRepo()
				service := templates.NewService(repo)

				err := op.call(t.Context(), service)
				assert.ErrorIs(t, err, authz.ErrNoPrincipal)
				assert.Zero(t, repo.reached, "a refused operation must not reach the repository")
			})
		})
	}
}

// The property that makes authorizing on a Domain recovered from a caller-supplied identifier
// sound: the parse can only narrow. A Principal that administers other.com, naming a Template of
// example.com, gets past the guard and then finds nothing, since the load is scoped alike.
func TestDomainScopedOperationsCannotReachAnotherDomainsTemplate(t *testing.T) {
	ctx := authz.NewContext(context.Background(), otherDomainAdmin)

	t.Run("GetTemplate", func(t *testing.T) {
		service := templates.NewService(seededRepo())
		_, err := service.GetTemplate(ctx, otherDomain, seededID)
		assert.ErrorIs(t, err, templates.ErrTemplateNotFound)
	})

	t.Run("UpdateTemplate", func(t *testing.T) {
		repo := seededRepo()
		service := templates.NewService(repo)

		_, err := service.UpdateTemplate(ctx, otherDomain, seededID, new("<p>owned</p>"), new("owned"))
		assert.ErrorIs(t, err, templates.ErrTemplateNotFound)

		// And the refusal was not just in the answer: the row is untouched.
		unchanged, err := repo.GetByID(t.Context(), seededID)
		require.NoError(t, err)
		assert.Equal(t, "<p>seeded</p>", unchanged.Html())
	})

	t.Run("DeleteTemplate", func(t *testing.T) {
		repo := seededRepo()
		service := templates.NewService(repo)

		_, err := service.DeleteTemplate(ctx, otherDomain, seededID)
		assert.ErrorIs(t, err, templates.ErrTemplateNotFound)

		_, err = repo.GetByID(t.Context(), seededID)
		assert.NoError(t, err, "a Template of another Domain must survive")
	})
}

// The operations still do what they did before they were guarded. Thin on purpose:
// the repository specification covers persistence, so what is left to check here is
// that the decorator returns what the operation produced rather than a zero value.
func TestServiceOperationsProduceWhatTheyUsedTo(t *testing.T) {
	ctx := authz.NewContext(context.Background(), rootAdmin)
	repo := seededRepo()
	service := templates.NewService(repo)

	created, err := service.CreateTemplate(ctx, homeDomain, "<p>fresh</p>", "fresh")
	require.NoError(t, err)
	assert.Equal(t, "<p>fresh</p>", created.Html())
	assert.Equal(t, homeDomain, created.DomainName())
	assert.Equal(t, templates.TypePersistent, created.Type())

	listed, total, err := service.GetTemplates(ctx, homeDomain, templates.Pagination{Take: 10})
	require.NoError(t, err)
	assert.Len(t, listed, 2)
	assert.Equal(t, 2, total)

	updated, err := service.UpdateTemplate(ctx, homeDomain, seededID, new("<p>edited</p>"), new("edited"))
	require.NoError(t, err)
	assert.Equal(t, "<p>edited</p>", updated.Html())
	assert.Equal(t, "edited", updated.Title())

	got, err := service.GetTemplate(ctx, homeDomain, seededID)
	require.NoError(t, err)
	assert.Equal(t, "<p>edited</p>", got.Html())

	deleted, err := service.DeleteTemplate(ctx, homeDomain, seededID)
	require.NoError(t, err)
	assert.Equal(t, seededID, deleted.TemplateID())

	_, err = service.GetTemplate(ctx, homeDomain, seededID)
	assert.ErrorIs(t, err, templates.ErrTemplateNotFound)
}

// countingRepo is the spec-checked in-memory Repository, counting how many times it was reached.
// The count is what lets a refusal be distinguished from a failure: an operation that never touched
// the store did not happen, whatever it returned.
type countingRepo struct {
	*templates.InMemRepository
	reached int
}

func seededRepo() *countingRepo {
	inner := templates.NewInMemRepository()
	seeded := templates.Load(templates.LoadParams{
		TemplateID: seededID,
		Html:       "<p>seeded</p>",
		Title:      "seeded",
		Domain:     homeDomain,
		Type:       templates.TypePersistent,
	})
	if err := inner.Create(context.Background(), seeded); err != nil {
		panic(err)
	}
	return &countingRepo{InMemRepository: inner}
}

func (r *countingRepo) Create(ctx context.Context, t *templates.Template) error {
	r.reached++
	return r.InMemRepository.Create(ctx, t)
}

func (r *countingRepo) Update(ctx context.Context, templateID string, fn templates.UpdateFunc) (*templates.Template, error) {
	r.reached++
	return r.InMemRepository.Update(ctx, templateID, fn)
}

func (r *countingRepo) Delete(ctx context.Context, templateID string) (*templates.Template, error) {
	r.reached++
	return r.InMemRepository.Delete(ctx, templateID)
}

func (r *countingRepo) GetByID(ctx context.Context, templateID string) (*templates.Template, error) {
	r.reached++
	return r.InMemRepository.GetByID(ctx, templateID)
}

func (r *countingRepo) FindByDomain(ctx context.Context, domain values.DomainName, templateID string) (*templates.Template, error) {
	r.reached++
	return r.InMemRepository.FindByDomain(ctx, domain, templateID)
}

func (r *countingRepo) List(ctx context.Context, domain values.DomainName, page templates.Pagination) ([]*templates.Template, error) {
	r.reached++
	return r.InMemRepository.List(ctx, domain, page)
}

func (r *countingRepo) ListPage(ctx context.Context, domain values.DomainName, page keyset.Page[string]) ([]*templates.Template, error) {
	r.reached++
	return r.InMemRepository.ListPage(ctx, domain, page)
}

func (r *countingRepo) Count(ctx context.Context, domain values.DomainName) (int, error) {
	r.reached++
	return r.InMemRepository.Count(ctx, domain)
}
