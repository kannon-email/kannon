package authz

import (
	"errors"
	"slices"
	"strings"
)

// Attribution is a claim naming who asked, on the far side of a caller Kannon
// cannot see into — a front-end that has its own people and hands their requests
// on.
//
// It is unverifiable in principle, not merely unverified: the people it names
// exist only in that calling system, so there is nothing here to check it
// against and no future version could. It is therefore recorded and never
// consulted — it can no more widen what a Principal may do than it can be
// verified — and it must never reach an authorization decision. If it ever did,
// a caller holding Attribute would gain the ability to choose its own authority.
//
// An Attribution is personal data. Whatever eventually persists one owes it a
// retention policy.
type Attribution string

// String returns the claim as written.
func (a Attribution) String() string {
	return string(a)
}

// Principal is who is making a request, as resolved by whatever authenticated
// it.
//
// A value object rather than a stored record: each authentication method
// populates one in its own way — an API Key by looking it up, a signed token by
// reading its claims — so what a Principal *is* never depends on how it arrived.
// It describes authority and does not decide; Can decides.
//
// The identifier names the credential the Principal came from, such as
// "<key-id>@<fqdn>". It is invariant under Attenuation, so narrowing changes
// what may be done and never who did it.
type Principal struct {
	id          string
	grants      []Grant
	attribution Attribution
}

// NewPrincipal resolves a Principal from an identifier and its Grants.
//
// A Principal with no Grants is permitted and can do nothing, which is a useful
// thing to be able to represent — an authenticated credential whose authority
// has been revoked is not the same as an unauthenticated request.
func NewPrincipal(id string, grants ...Grant) (Principal, error) {
	if strings.TrimSpace(id) == "" {
		return Principal{}, errors.New("principal id is required")
	}
	return Principal{
		id:     id,
		grants: slices.Clone(grants),
	}, nil
}

// MustNewPrincipal is NewPrincipal for tests and package-level values.
func MustNewPrincipal(id string, grants ...Grant) Principal {
	p, err := NewPrincipal(id, grants...)
	if err != nil {
		panic(err)
	}
	return p
}

// ID returns the identifier of the credential this Principal came from.
func (p Principal) ID() string {
	return p.id
}

// Grants returns a copy of the Principal's Grants, so that a caller cannot
// widen its own authority by writing to the slice it was handed.
func (p Principal) Grants() []Grant {
	return slices.Clone(p.grants)
}

// Attribution returns the claim about who asked, or empty if none was made.
func (p Principal) Attribution() Attribution {
	return p.attribution
}

// WithAttribution returns a copy of the Principal carrying a claim about who
// asked.
//
// This performs **no** check, and that is deliberate rather than an oversight.
// Entitlement to make a claim depends on the Resource being acted on, which is
// not known here, so it is verified where it is known: Guard refuses an
// operation whose Principal carries an Attribution it does not hold Attribute
// for. Setting one therefore cannot smuggle anything past a guard — it can only
// cause the guarded operation to be refused.
func (p Principal) WithAttribution(a Attribution) Principal {
	p.attribution = a
	p.grants = slices.Clone(p.grants)
	return p
}

// String renders the Principal for display and logging: its identifier, its
// Grants, and the Attribution it claims, if any.
//
// The identifier is always rendered whether or not an Attribution accompanies
// it, so a record never leaves the question of who acted unanswered and an
// authenticated identity is never confused with an asserted one.
func (p Principal) String() string {
	var b strings.Builder
	b.WriteString(p.id)

	b.WriteString(" [")
	for i, g := range p.grants {
		if i > 0 {
			b.WriteString(", ")
		}
		b.WriteString(g.String())
	}
	b.WriteString("]")

	if p.attribution != "" {
		b.WriteString(" claiming ")
		b.WriteString(p.attribution.String())
	}

	return b.String()
}
