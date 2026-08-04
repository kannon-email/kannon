package admintoken_test

import (
	"testing"

	"github.com/kannon-email/kannon/internal/admintoken"
	"github.com/kannon-email/kannon/internal/authz"
	"github.com/kannon-email/kannon/internal/values"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const secret = "s3cr3t-admin-token"

// A blank secret is refused at the boundary rather than left to fail every comparison later: it
// would authenticate the empty header of a request carrying no credential at all, which is the
// single failure this package must not have.
func TestParseRefusesABlankSecret(t *testing.T) {
	for _, s := range []string{"", " ", "\n", "\t  \n"} {
		_, err := admintoken.Parse(s)
		assert.Error(t, err, "expected %q to be refused as a secret", s)
	}
}

// The secret an operator mounts from a file or a Kubernetes Secret carries a trailing newline
// often enough that keeping it would produce a credential nobody can present.
func TestParseTrimsSurroundingWhitespace(t *testing.T) {
	token, err := admintoken.Parse("  " + secret + "\n")
	require.NoError(t, err)

	_, err = token.Authenticate(secret)
	assert.NoError(t, err, "the trimmed secret is the credential")
}

func TestAuthenticate(t *testing.T) {
	tests := []struct {
		name      string
		token     admintoken.Token
		presented string
		wantErr   bool
	}{
		{
			name:      "the configured secret",
			token:     admintoken.MustParse(secret),
			presented: secret,
		},
		{
			name:      "another secret",
			token:     admintoken.MustParse(secret),
			presented: "not-the-token",
			wantErr:   true,
		},
		{
			// A prefix, because a comparison written with strings.HasPrefix — or
			// with a length nobody checked — would accept it.
			name:      "a prefix of the configured secret",
			token:     admintoken.MustParse(secret),
			presented: secret[:len(secret)-1],
			wantErr:   true,
		},
		{
			name:      "nothing at all, which is what an unauthenticated request presents",
			token:     admintoken.MustParse(secret),
			presented: "",
			wantErr:   true,
		},
		{
			// The zero Token cannot arrive from Parse, but it is what a struct field
			// nobody assigned holds, and it must authenticate nothing — least of all
			// the empty header that would otherwise equal its empty secret.
			name:      "the zero Token, presented with nothing",
			token:     admintoken.Token{},
			presented: "",
			wantErr:   true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			p, err := tc.token.Authenticate(tc.presented)

			if tc.wantErr {
				assert.ErrorIs(t, err, admintoken.ErrInvalidToken)
				assert.Equal(t, authz.Principal{}, p, "a refusal must hand back no authority")
				return
			}

			require.NoError(t, err)
			assert.Equal(t, "admin-token", p.ID())
		})
	}
}

// What the token confers, asked of Can rather than read off the Grant: it resolves to admin on the
// root (ADR 0009), which reaches every Domain — including one created after the token was
// configured, since the Anchor names no Domain — and holds every Action, attribute included
// (ADR 0008).
func TestTheAdminPrincipalIsAdminEverywhere(t *testing.T) {
	p := admintoken.AdminPrincipal()
	domain := values.MustParse("example.com")

	assert.True(t, authz.Can(p, authz.Create, authz.Domains()))
	assert.True(t, authz.Can(p, authz.List, authz.Domains()))
	assert.True(t, authz.Can(p, authz.Update, authz.Domain(domain)))
	assert.True(t, authz.Can(p, authz.Delete, authz.APIKey(domain, "key-id")))
	assert.True(t, authz.Can(p, authz.Read, authz.Stats(domain)))
	assert.True(t, authz.Can(p, authz.Create, authz.Batches(domain)))
	assert.True(t, authz.Can(p, authz.Attribute, authz.Domain(domain)),
		"the token is what a front-end holds, so it is what may name the person who asked")

	// The credential itself names nobody: a claim belongs to one request, and a
	// Principal that arrived carrying one would name the same person for every
	// request made with the token.
	assert.Empty(t, p.Attribution())
}
