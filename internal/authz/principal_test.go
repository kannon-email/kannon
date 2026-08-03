package authz_test

import (
	"testing"

	"github.com/kannon-email/kannon/internal/authz"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewPrincipalRequiresAnIdentifier(t *testing.T) {
	// A Principal with no Grants is permitted and can do nothing, which is a
	// useful thing to represent: a credential whose authority has been revoked is
	// not the same as an unauthenticated request.
	p, err := authz.NewPrincipal("key-1@example.com")
	require.NoError(t, err)
	assert.Equal(t, "key-1@example.com", p.ID())

	_, err = authz.NewPrincipal("   ")
	assert.Error(t, err)

	assert.Panics(t, func() { authz.MustNewPrincipal("") })
}

// The identifier is rendered whether or not an Attribution accompanies it, so a
// record never leaves the question of who acted unanswered and an authenticated
// identity is never confused with an asserted one.
func TestPrincipalRendersItsCredentialGrantsAndClaim(t *testing.T) {
	p := authz.MustNewPrincipal("key-1@example.com",
		authz.MustNewGrant(authz.RoleSender, authz.DomainAnchor(example)),
		authz.MustNewGrant(authz.RoleAdmin, authz.RootAnchor()),
	)

	assert.Equal(t, "key-1@example.com [sender on domains/example.com, admin on *]", p.String())
	assert.Empty(t, p.Attribution())

	claiming := p.WithAttribution("alice@corp.com")
	assert.Equal(t,
		"key-1@example.com [sender on domains/example.com, admin on *] claiming alice@corp.com",
		claiming.String())
	assert.Equal(t, "alice@corp.com", claiming.Attribution().String())

	// The original is untouched, and the Grants it hands out are a copy: a caller
	// must not be able to widen its own authority by writing to the slice it was
	// given.
	assert.Empty(t, p.Attribution())
	grants := p.Grants()
	require.Len(t, grants, 2)
	grants[0] = authz.Grant{}
	assert.Equal(t, "sender on domains/example.com", p.Grants()[0].String())
}

// The vocabulary is closed: six verbs, and giving Kannon a new kind of thing to
// manage adds a path segment rather than an Action.
func TestParseActionValidatesTheClosedVocabulary(t *testing.T) {
	for _, want := range append(resourceActions, authz.Attribute) {
		got, err := authz.ParseAction(want.String())
		require.NoError(t, err)
		assert.Equal(t, want, got)
	}

	for _, unknown := range []string{"", "send", "manage", "create-template", "impersonate", "Create"} {
		_, err := authz.ParseAction(unknown)
		assert.Error(t, err, "expected %q to be refused", unknown)
	}
}
