package apikeys_test

import (
	"context"
	"testing"

	"github.com/kannon-email/kannon/internal/apikeys"
	apikeyshelpers "github.com/kannon-email/kannon/internal/apikeys/helpers"
	"github.com/kannon-email/kannon/internal/authz"
	"github.com/kannon-email/kannon/internal/values"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// otherDomain is the Domain nothing in these tests belongs to, so that a Principal
// with real authority over the wrong place can be told apart from one with no
// authority at all.
var otherDomain = values.MustParse("other.example.com")

// Principals, one Grant each, named for the authority they hold.
var (
	rootAdmin        = authz.MustNewPrincipal("root-admin", authz.MustNewGrant(authz.RoleAdmin, authz.RootAnchor()))
	everyDomainAdmin = authz.MustNewPrincipal("every-domain-admin", authz.MustNewGrant(authz.RoleAdmin, authz.AllDomainsAnchor()))
	homeDomainAdmin  = authz.MustNewPrincipal("home-domain-admin", authz.MustNewGrant(authz.RoleAdmin, authz.DomainAnchor(testDomain)))
	otherDomainAdmin = authz.MustNewPrincipal("other-domain-admin", authz.MustNewGrant(authz.RoleAdmin, authz.DomainAnchor(otherDomain)))
	senderOnly       = authz.MustNewPrincipal("sender-only", authz.MustNewGrant(authz.RoleSender, authz.DomainAnchor(testDomain)))
	noGrants         = authz.MustNewPrincipal("no-grants")
)

// TestServiceAuthorization is the table that says what each operation demands.
//
// The sender-only Principal is the one that matters most here, and it is refused by
// all four. It is exactly what an API Key resolves to under ADR 0008 — sender on the
// key's own Domain — so this is the assertion that a stolen sending key cannot mint
// itself a second one, list its siblings, or revoke anybody.
func TestServiceAuthorization(t *testing.T) {
	ops := []struct {
		name  string
		call  func(context.Context, *apikeys.Service, apikeys.KeyRef) error
		allow []authz.Principal
		deny  []authz.Principal
	}{
		{
			name: "CreateKey",
			call: func(ctx context.Context, s *apikeys.Service, _ apikeys.KeyRef) error {
				_, err := s.CreateKey(ctx, testDomain, "another-key", nil)
				return err
			},
			allow: []authz.Principal{rootAdmin, everyDomainAdmin, homeDomainAdmin},
			deny:  []authz.Principal{otherDomainAdmin, senderOnly, noGrants},
		},
		{
			name: "ListKeys",
			call: func(ctx context.Context, s *apikeys.Service, _ apikeys.KeyRef) error {
				_, _, err := s.ListKeys(ctx, testDomain, false, apikeys.Pagination{Limit: 10})
				return err
			},
			allow: []authz.Principal{rootAdmin, everyDomainAdmin, homeDomainAdmin},
			deny:  []authz.Principal{otherDomainAdmin, senderOnly, noGrants},
		},
		{
			name: "GetKey",
			call: func(ctx context.Context, s *apikeys.Service, ref apikeys.KeyRef) error {
				_, err := s.GetKey(ctx, ref)
				return err
			},
			allow: []authz.Principal{rootAdmin, everyDomainAdmin, homeDomainAdmin},
			deny:  []authz.Principal{otherDomainAdmin, senderOnly, noGrants},
		},
		{
			name: "DeactivateKey",
			call: func(ctx context.Context, s *apikeys.Service, ref apikeys.KeyRef) error {
				_, err := s.DeactivateKey(ctx, ref)
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
					service, ref := seededService(t)

					err := op.call(authz.NewContext(t.Context(), p), service, ref)
					require.NoError(t, err)
				})
			}

			for _, p := range op.deny {
				t.Run("refuses "+p.ID(), func(t *testing.T) {
					service, ref := seededService(t)

					err := op.call(authz.NewContext(t.Context(), p), service, ref)
					assert.ErrorIs(t, err, authz.ErrForbidden)
				})
			}

			// Nothing authenticated the request, which is what the Admin API's
			// interceptor leaves behind when it refuses a token. It is a separate error
			// from a refusal because they are separate operational problems.
			t.Run("refuses a request with no Principal", func(t *testing.T) {
				service, ref := seededService(t)

				err := op.call(t.Context(), service, ref)
				assert.ErrorIs(t, err, authz.ErrNoPrincipal)
			})
		})
	}
}

// A refused DeactivateKey leaves the credential usable, and a refused CreateKey mints
// nothing. Asserted apart from the table because the error alone would not prove it:
// a guard that ran the operation and then refused to return the result would satisfy
// every assertion above while having already revoked somebody's key.
func TestRefusedOperationsChangeNothing(t *testing.T) {
	service, ref := seededService(t)
	refused := authz.NewContext(context.Background(), senderOnly)
	authorized := authz.NewContext(context.Background(), rootAdmin)

	_, err := service.DeactivateKey(refused, ref)
	assert.ErrorIs(t, err, authz.ErrForbidden)

	still, err := service.GetKey(authorized, ref)
	require.NoError(t, err)
	assert.True(t, still.IsActiveStatus(), "a refused deactivation must leave the key usable")

	_, err = service.CreateKey(refused, testDomain, "smuggled", nil)
	assert.ErrorIs(t, err, authz.ErrForbidden)

	_, total, err := service.ListKeys(authorized, testDomain, false, apikeys.Pagination{Limit: 10})
	require.NoError(t, err)
	assert.Equal(t, 1, total, "a refused creation must leave the Domain with the one key it had")
}

// Authenticating a key is deliberately unguarded: it is the step that decides who the
// caller is, so it cannot require the answer it produces. What it discloses on failure
// is the same for every cause, which is what makes that safe.
func TestValidateForAuthNeedsNoPrincipal(t *testing.T) {
	repo := apikeyshelpers.NewInMemoryRepository()
	service := apikeys.NewService(repo)

	created, err := service.CreateKey(authz.NewContext(context.Background(), rootAdmin), testDomain, "test-key", nil)
	require.NoError(t, err)

	// A bare context: nothing authenticated this, because this is what authenticates.
	validated, err := service.ValidateForAuth(context.Background(), testDomain, created.PlaintextKey)
	require.NoError(t, err)
	assert.Equal(t, created.Key.ID(), validated.ID())

	_, err = service.ValidateForAuth(context.Background(), testDomain, "k_wrong12345678901234567890")
	assert.ErrorIs(t, err, apikeys.ErrKeyNotFound)
}

// seededService returns a Service holding exactly one key of testDomain, and a
// reference to it. The seeding goes through the guarded CreateKey with root authority
// rather than around it, so a change that broke creation cannot leave these tests
// passing against a store nothing wrote to.
func seededService(t *testing.T) (*apikeys.Service, apikeys.KeyRef) {
	t.Helper()

	service := apikeys.NewService(apikeyshelpers.NewInMemoryRepository())
	created, err := service.CreateKey(authz.NewContext(t.Context(), rootAdmin), testDomain, "seeded-key", nil)
	require.NoError(t, err)

	return service, apikeys.NewKeyRef(testDomain, created.Key.ID())
}
