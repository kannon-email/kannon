package domains_test

import (
	"context"
	"testing"
	"time"

	"github.com/kannon-email/kannon/internal/authz"
	"github.com/kannon-email/kannon/internal/domains"
	"github.com/kannon-email/kannon/internal/tracking"
	"github.com/kannon-email/kannon/internal/values"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Fixture Domains. MustParse is right for package-level values in a test: a bad one
// is a bug in this file, not input.
var (
	homeDomain  = values.MustParse("example.com")
	otherDomain = values.MustParse("other.com")
)

// Principals, one Grant each, named for the authority they hold.
var (
	rootAdmin        = authz.MustNewPrincipal("root-admin", authz.MustNewGrant(authz.RoleAdmin, authz.RootAnchor()))
	everyDomainAdmin = authz.MustNewPrincipal("every-domain-admin", authz.MustNewGrant(authz.RoleAdmin, authz.AllDomainsAnchor()))
	homeDomainAdmin  = authz.MustNewPrincipal("home-domain-admin", authz.MustNewGrant(authz.RoleAdmin, authz.DomainAnchor(homeDomain)))
	otherDomainAdmin = authz.MustNewPrincipal("other-domain-admin", authz.MustNewGrant(authz.RoleAdmin, authz.DomainAnchor(otherDomain)))
	senderOnly       = authz.MustNewPrincipal("sender-only", authz.MustNewGrant(authz.RoleSender, authz.DomainAnchor(homeDomain)))
	noGrants         = authz.MustNewPrincipal("no-grants")
)

// TestServiceAuthorization is the table that says what each operation demands. CreateDomain and
// GetDomains admit only the root Grant: they act on the Domains collection, and a pattern longer
// than the Resource covers nothing — which is what let ADR 0008 delete the System tier.
func TestServiceAuthorization(t *testing.T) {
	policy := tracking.Policy{Opens: tracking.ModeOff, Links: tracking.ModeOff}

	ops := []struct {
		name  string
		call  func(context.Context, *domains.Service) error
		allow []authz.Principal
		deny  []authz.Principal
	}{
		{
			name: "CreateDomain",
			call: func(ctx context.Context, s *domains.Service) error {
				_, err := s.CreateDomain(ctx, values.MustParse("fresh.example.com"))
				return err
			},
			allow: []authz.Principal{rootAdmin},
			deny:  []authz.Principal{everyDomainAdmin, homeDomainAdmin, otherDomainAdmin, senderOnly, noGrants},
		},
		{
			name: "GetDomains",
			call: func(ctx context.Context, s *domains.Service) error {
				_, err := s.GetDomains(ctx)
				return err
			},
			allow: []authz.Principal{rootAdmin},
			deny:  []authz.Principal{everyDomainAdmin, homeDomainAdmin, otherDomainAdmin, senderOnly, noGrants},
		},
		{
			name: "GetDomain",
			call: func(ctx context.Context, s *domains.Service) error {
				_, err := s.GetDomain(ctx, homeDomain)
				return err
			},
			allow: []authz.Principal{rootAdmin, everyDomainAdmin, homeDomainAdmin},
			deny:  []authz.Principal{otherDomainAdmin, senderOnly, noGrants},
		},
		{
			name: "SetTrackingPolicy",
			call: func(ctx context.Context, s *domains.Service) error {
				_, err := s.SetTrackingPolicy(ctx, homeDomain, policy)
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
					service := domains.NewService(repo)

					err := op.call(authz.NewContext(t.Context(), p), service)
					require.NoError(t, err)
					assert.Positive(t, repo.reached, "the operation was authorized but never ran")
				})
			}

			for _, p := range op.deny {
				t.Run("refuses "+p.ID(), func(t *testing.T) {
					repo := seededRepo()
					service := domains.NewService(repo)

					err := op.call(authz.NewContext(t.Context(), p), service)
					assert.ErrorIs(t, err, authz.ErrForbidden)
					assert.Zero(t, repo.reached, "a refused operation must not reach the repository")
				})
			}

			// Nothing authenticated the request, which is what the Admin API's
			// interceptor leaves behind when it refuses a token. It is a separate error
			// from a refusal because they are separate operational problems.
			t.Run("refuses a request with no Principal", func(t *testing.T) {
				repo := seededRepo()
				service := domains.NewService(repo)

				err := op.call(t.Context(), service)
				assert.ErrorIs(t, err, authz.ErrNoPrincipal)
				assert.Zero(t, repo.reached, "a refused operation must not reach the repository")
			})
		})
	}
}

// A refused SetTrackingPolicy leaves the Policy as it was. Asserted separately because this is the
// one operation here whose failure mode is silent: a Domain whose ceiling was lowered without
// authority would keep serving mail, just tracking less of it, and nothing would look broken.
func TestRefusedSetTrackingPolicyChangesNothing(t *testing.T) {
	repo := seededRepo()
	service := domains.NewService(repo)

	ctx := authz.NewContext(context.Background(), otherDomainAdmin)
	_, err := service.SetTrackingPolicy(ctx, homeDomain, tracking.Policy{Opens: tracking.ModeOff, Links: tracking.ModeOff})
	assert.ErrorIs(t, err, authz.ErrForbidden)

	unchanged := repo.byName[homeDomain]
	assert.Equal(t, tracking.ModeIdentified, unchanged.TrackingPolicy().Opens)
}

// The operations still do what they did before they were guarded — enough to show the
// decorator returns what the operation produced rather than a zero value.
func TestServiceOperationsProduceWhatTheyUsedTo(t *testing.T) {
	ctx := authz.NewContext(context.Background(), rootAdmin)
	repo := seededRepo()
	service := domains.NewService(repo)

	created, err := service.CreateDomain(ctx, values.MustParse("fresh.example.com"))
	require.NoError(t, err)
	assert.Equal(t, "fresh.example.com", created.Domain())
	assert.NotEmpty(t, created.DkimPublicKey(), "a new Domain carries a freshly generated key pair")

	all, err := service.GetDomains(ctx)
	require.NoError(t, err)
	assert.Len(t, all, 2)

	got, err := service.GetDomain(ctx, homeDomain)
	require.NoError(t, err)
	assert.Equal(t, homeDomain, got.Name())

	updated, err := service.SetTrackingPolicy(ctx, homeDomain, tracking.Policy{Opens: tracking.ModeAnonymous, Links: tracking.ModeOff})
	require.NoError(t, err)
	assert.Equal(t, tracking.ModeAnonymous, updated.TrackingPolicy().Opens)
}

// fakeRepo is an in-memory Repository for these tests. It counts how many times it
// was reached, which is what distinguishes an operation that was refused from one
// that ran and then failed.
type fakeRepo struct {
	byName  map[values.DomainName]*domains.Domain
	reached int
}

func seededRepo() *fakeRepo {
	return &fakeRepo{
		byName: map[values.DomainName]*domains.Domain{
			homeDomain: domains.Load(domains.LoadParams{
				ID:            1,
				Domain:        homeDomain,
				DkimPublicKey: "seeded-public-key",
				CreatedAt:     time.Now(),
				Tracking:      tracking.Policy{Opens: tracking.ModeIdentified, Links: tracking.ModeIdentified},
			}),
		},
	}
}

func (r *fakeRepo) Create(_ context.Context, d *domains.Domain) error {
	r.reached++
	r.byName[d.Name()] = d
	return nil
}

func (r *fakeRepo) SetTrackingPolicy(_ context.Context, domain values.DomainName, p tracking.Policy) (*domains.Domain, error) {
	r.reached++
	d, ok := r.byName[domain]
	if !ok {
		return nil, domains.ErrDomainNotFound
	}
	updated := domains.Load(domains.LoadParams{
		ID:             d.ID(),
		Domain:         d.Name(),
		DkimPrivateKey: d.DkimPrivateKey(),
		DkimPublicKey:  d.DkimPublicKey(),
		CreatedAt:      d.CreatedAt(),
		Tracking:       p,
	})
	r.byName[domain] = updated
	return updated, nil
}

func (r *fakeRepo) FindByName(_ context.Context, domain values.DomainName) (*domains.Domain, error) {
	r.reached++
	d, ok := r.byName[domain]
	if !ok {
		return nil, domains.ErrDomainNotFound
	}
	return d, nil
}

func (r *fakeRepo) List(_ context.Context) ([]*domains.Domain, error) {
	r.reached++
	all := make([]*domains.Domain, 0, len(r.byName))
	for _, d := range r.byName {
		all = append(all, d)
	}
	return all, nil
}
