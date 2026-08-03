package authz_test

import (
	"testing"

	"github.com/kannon-email/kannon/internal/authz"
	"github.com/kannon-email/kannon/internal/values"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The two grantable Anchor kinds, crossed with the Roles whose rules fit them.
func TestNewGrantAccepts(t *testing.T) {
	tests := []struct {
		name   string
		role   authz.RoleName
		anchor authz.Anchor
	}{
		{"admin on the root", authz.RoleAdmin, authz.RootAnchor()},
		{"admin on every Domain", authz.RoleAdmin, authz.AllDomainsAnchor()},
		{"admin on one Domain", authz.RoleAdmin, authz.DomainAnchor(example)},
		{"sender on one Domain", authz.RoleSender, authz.DomainAnchor(example)},
		{"sender on every Domain", authz.RoleSender, authz.AllDomainsAnchor()},
		// AnchorOf(Domain(f)) is the same Anchor by a different route: the one
		// Attenuation takes when it narrows to a concrete Domain.
		{"admin on the Anchor of a Domain", authz.RoleAdmin, authz.AnchorOf(authz.Domain(other))},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			g, err := authz.NewGrant(tc.role, tc.anchor)
			require.NoError(t, err)
			assert.Equal(t, tc.role, g.Role())
			assert.Equal(t, tc.anchor.String(), g.Anchor().String())
		})
	}
}

// Every refusal, with the message asserted where the message is the point: this is what an
// operator sees when issuing a credential, and the near misses — the bare domains collection, a
// typed Role on the wrong kind of Anchor — are the ones whose meaning would differ silently.
func TestNewGrantRefuses(t *testing.T) {
	tests := []struct {
		name   string
		role   authz.RoleName
		anchor authz.Anchor
		msg    string
	}{
		{
			// Checked before the Anchor, since nothing else can be judged
			// without knowing what Role was meant.
			name:   "a Role the catalogue does not hold",
			role:   authz.RoleName("provisioner"),
			anchor: authz.RootAnchor(),
			msg:    `unknown role "provisioner"`,
		},
		{
			name:   "an empty Role name",
			role:   authz.RoleName(""),
			anchor: authz.RootAnchor(),
			msg:    `unknown role ""`,
		},
		{
			// The one refusal that names the alternative, because this is what
			// an author reaches for to say "every Domain".
			name:   "the bare domains collection",
			role:   authz.RoleAdmin,
			anchor: authz.AnchorOf(authz.Domains()),
			msg:    `anchor "domains" is not grantable; did you mean "domains/*"?`,
		},
		{
			name:   "a Domain's Batches",
			role:   authz.RoleAdmin,
			anchor: authz.AnchorOf(authz.Batches(example)),
			msg:    `anchor "domains/example.com/batches" is not grantable; a Grant is issued on the root ("*") or on a Domain ("domains/<name>" or "domains/*")`,
		},
		{
			name:   "one Template",
			role:   authz.RoleAdmin,
			anchor: authz.AnchorOf(authz.Template(example, "welcome")),
			msg:    `anchor "domains/example.com/templates/welcome" is not grantable; a Grant is issued on the root ("*") or on a Domain ("domains/<name>" or "domains/*")`,
		},
		{
			name:   "a Templates collection",
			role:   authz.RoleAdmin,
			anchor: authz.AnchorOf(authz.Templates(example)),
			msg:    `anchor "domains/example.com/templates" is not grantable; a Grant is issued on the root ("*") or on a Domain ("domains/<name>" or "domains/*")`,
		},
		{
			name:   "an API Key",
			role:   authz.RoleAdmin,
			anchor: authz.AnchorOf(authz.APIKey(example, "key-1")),
			msg:    `anchor "domains/example.com/apikeys/key-1" is not grantable; a Grant is issued on the root ("*") or on a Domain ("domains/<name>" or "domains/*")`,
		},
		{
			name:   "the counters",
			role:   authz.RoleAdmin,
			anchor: authz.AnchorOf(authz.AggregatedStats(example)),
			msg:    `anchor "domains/example.com/stats/aggregated" is not grantable; a Grant is issued on the root ("*") or on a Domain ("domains/<name>" or "domains/*")`,
		},
		{
			// The zero Anchor names nothing, and naming nothing is not the same
			// as naming everything. The root is a flag precisely so that this
			// row fails closed.
			name:   "the zero Anchor",
			role:   authz.RoleAdmin,
			anchor: authz.Anchor{},
			msg:    `anchor "" is not grantable; a Grant is issued on the root ("*") or on a Domain ("domains/<name>" or "domains/*")`,
		},
		{
			name:   "the Anchor of a zero Resource",
			role:   authz.RoleAdmin,
			anchor: authz.AnchorOf(authz.Resource{}),
			msg:    `anchor "" is not grantable; a Grant is issued on the root ("*") or on a Domain ("domains/<name>" or "domains/*")`,
		},
		{
			// Shaped like a Domain Anchor and refused anyway: a zero domain
			// name reaching a constructor is a programming error, and it must
			// not yield a Grant that reaches whatever "domains/" alone would.
			name:   "a Domain with a zero domain name",
			role:   authz.RoleAdmin,
			anchor: authz.DomainAnchor(values.DomainName{}),
			msg:    `anchor "domains/" is not grantable; a Grant is issued on the root ("*") or on a Domain ("domains/<name>" or "domains/*")`,
		},
		{
			// The soundness case ADR 0008 owes the model: a typed Role granted off its
			// declared kind. sender's rule names batches, which composes only beneath a
			// Domain — at the root it would compose a bare "batches" that means nothing here.
			name:   "a domain-scoped Role on the root",
			role:   authz.RoleSender,
			anchor: authz.RootAnchor(),
			msg:    `role "sender" is domain-scoped and cannot be anchored on the root ("*")`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := authz.NewGrant(tc.role, tc.anchor)
			require.Error(t, err)
			assert.EqualError(t, err, tc.msg)
		})
	}
}

func TestMustNewGrantPanicsOnARefusedGrant(t *testing.T) {
	assert.Panics(t, func() { authz.MustNewGrant(authz.RoleSender, authz.RootAnchor()) })
	assert.NotPanics(t, func() { authz.MustNewGrant(authz.RoleSender, authz.DomainAnchor(example)) })
}

func TestGrantRendersItsRoleAndAnchor(t *testing.T) {
	tests := []struct {
		grant authz.Grant
		want  string
	}{
		{authz.MustNewGrant(authz.RoleAdmin, authz.RootAnchor()), "admin on *"},
		{authz.MustNewGrant(authz.RoleAdmin, authz.AllDomainsAnchor()), "admin on domains/*"},
		{authz.MustNewGrant(authz.RoleSender, authz.DomainAnchor(example)), "sender on domains/example.com"},
	}

	for _, tc := range tests {
		t.Run(tc.want, func(t *testing.T) {
			assert.Equal(t, tc.want, tc.grant.String())
		})
	}
}

func TestParseRoleNameValidatesAgainstTheCatalogue(t *testing.T) {
	for _, name := range authz.RoleNames() {
		parsed, err := authz.ParseRoleName(string(name))
		require.NoError(t, err)
		assert.Equal(t, name, parsed)
	}

	_, err := authz.ParseRoleName("domain-admin")
	assert.EqualError(t, err, `unknown role "domain-admin"`)
}
