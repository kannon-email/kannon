// Package admintoken is the credential that closes Kannon's open surfaces: one shared secret an
// operator configures, which resolves to admin on the root and authenticates the Admin API and
// both Stats API versions. Deliberately the smallest thing that ends the open access ADR 0008
// scaffolded for — one secret, no issuance, no revocation short of a restart — and the seam a
// per-operator credential replaces without any call site learning of it (ADR 0009).
package admintoken

import (
	"crypto/subtle"
	"errors"
	"strings"

	"github.com/kannon-email/kannon/internal/authz"
)

// principalID names the credential a request authenticated with rather than the authority it
// confers. Every operation of every Domain is recorded against this one name, which is precisely
// what makes a shared token an interim answer: it says a holder acted, never which one.
const principalID = "admin-token"

// ErrInvalidToken is what Authenticate answers to anything that is not the configured secret,
// including nothing at all. The two are one error on purpose: a caller must not learn from the
// refusal whether what it presented was recognised as a token.
var ErrInvalidToken = errors.New("invalid admin token")

// Token is the configured secret, held so that nothing but Authenticate can read it back — a
// value that can be compared against a request and cannot be logged, rendered or returned. The
// zero Token authenticates nothing, so a Token that was never configured fails closed.
type Token struct {
	secret string
}

// Parse resolves a configured value into a Token, refusing a blank one: an empty secret would
// match the empty header every unauthenticated request carries, which is the whole of what this
// package exists to prevent. Surrounding whitespace is trimmed, since a secret mounted from a file
// or a Kubernetes Secret arrives with a trailing newline often enough that treating it as part of
// the credential would leave an operator with a token nobody can present.
func Parse(s string) (Token, error) {
	secret := strings.TrimSpace(s)
	if secret == "" {
		return Token{}, errors.New("admin token is empty")
	}
	return Token{secret: secret}, nil
}

// MustParse is Parse for tests and package-level values, where a blank
// secret is a programming error rather than an operator's.
func MustParse(s string) Token {
	t, err := Parse(s)
	if err != nil {
		panic(err)
	}
	return t
}

// Authenticate resolves a presented secret to the Principal it confers. The comparison is
// constant-time: the secret is long-lived and shared, so a timing oracle over it is worth more to
// an attacker than one over any other credential Kannon holds. The presented value is compared as
// it arrived — a header value reaches here already trimmed by the HTTP layer, and trimming again
// would mean a secret and the header claiming it were normalised by different rules.
func (t Token) Authenticate(presented string) (authz.Principal, error) {
	if t.secret == "" {
		return authz.Principal{}, ErrInvalidToken
	}
	if subtle.ConstantTimeCompare([]byte(t.secret), []byte(presented)) != 1 {
		return authz.Principal{}, ErrInvalidToken
	}
	return AdminPrincipal(), nil
}

// AdminPrincipal is the authority the token confers: admin on the root, so every Domain and
// everything beneath it. It carries no Attribution, and not as a special case somebody could
// forget — admin holds no attribute, so one set on it causes a Guard to refuse (ADR 0008).
func AdminPrincipal() authz.Principal {
	return authz.MustNewPrincipal(principalID, authz.MustNewGrant(authz.RoleAdmin, authz.RootAnchor()))
}
