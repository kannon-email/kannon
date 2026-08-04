package apikeys_test

import (
	"testing"

	"github.com/kannon-email/kannon/internal/apikeys"
	"github.com/kannon-email/kannon/internal/authz"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestAPIKeyResolvesToSenderOnItsOwnDomain is the shape of the authority an
// authenticated API Key confers: one Grant, sender, anchored on the key's own
// Domain (ADR 0008).
func TestAPIKeyResolvesToSenderOnItsOwnDomain(t *testing.T) {
	created, err := apikeys.NewAPIKey(testDomain, "test-key", nil)
	require.NoError(t, err)

	p, err := created.Key.Principal()
	require.NoError(t, err)

	// The identifier names the credential, not its Domain: the Grant below is
	// identical for every key of one Domain, so this is the only thing that tells
	// two of them apart in a log.
	assert.Equal(t, created.Key.ID().String()+"@"+testDomain.String(), p.ID())

	grants := p.Grants()
	require.Len(t, grants, 1, "a key carries exactly one fixed Grant and nothing else")
	assert.Equal(t, authz.RoleSender, grants[0].Role())
	assert.Equal(t, "domains/"+testDomain.String(), grants[0].Anchor().String())

	// No Attribution, and not because anything checked: there is no trusted
	// front-end behind an API Key to speak for (ADR 0008).
	assert.Empty(t, p.Attribution())
}

// TestAPIKeyPrincipalCanOnlySend enumerates what that one Grant does and does not reach. The
// refusals are the point: this is the Principal a stolen sending key carries, so it cannot register
// a Domain, read its own, rewrite Templates, mint a second credential or send for anybody else.
func TestAPIKeyPrincipalCanOnlySend(t *testing.T) {
	created, err := apikeys.NewAPIKey(testDomain, "test-key", nil)
	require.NoError(t, err)

	p, err := created.Key.Principal()
	require.NoError(t, err)

	assert.True(t, authz.Can(p, authz.Create, authz.Batches(testDomain)),
		"a key must be able to send for its own Domain")

	refused := []struct {
		what     string
		action   authz.Action
		resource authz.Resource
	}{
		{"register a Domain", authz.Create, authz.Domains()},
		{"enumerate Domains", authz.List, authz.Domains()},
		{"read its own Domain", authz.Read, authz.Domain(testDomain)},
		{"change its own Domain's Tracking Policy", authz.Update, authz.Domain(testDomain)},
		{"rewrite its own Domain's Templates", authz.Update, authz.Templates(testDomain)},
		{"mint another key for its own Domain", authz.Create, authz.APIKeys(testDomain)},
		{"read its own Domain's statistics", authz.Read, authz.Stats(testDomain)},
		{"send for another Domain", authz.Create, authz.Batches(otherDomain)},
		// attribute is in the vocabulary and no seeded Role holds it, so a key
		// naming a person would have its operation refused rather than recorded
		// under a name nothing verified.
		{"name who asked", authz.Attribute, authz.Batches(testDomain)},
	}

	for _, tc := range refused {
		t.Run(tc.what, func(t *testing.T) {
			assert.False(t, authz.Can(p, tc.action, tc.resource))
		})
	}
}
