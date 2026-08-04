package authz

import (
	"errors"
	"slices"
	"strings"
)

// Principal is who is making a request, as resolved by whatever authenticated it: a value
// object, so what a Principal is never depends on how it arrived. Its identifier names the
// credential and is invariant under Attenuation. It describes authority; Can decides.
type Principal struct {
	id          string
	grants      []Grant
	attribution Attribution
}

// NewPrincipal resolves a Principal from an identifier and its Grants. One with no Grants is
// permitted and can do nothing, which is worth representing: an authenticated credential
// whose authority has been revoked is not the same as an unauthenticated request.
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

// WithAttribution returns a copy of the Principal carrying a claim about who asked. It checks
// nothing, deliberately: entitlement depends on the Resource, so Guard verifies it there.
// Setting one can therefore only cause the guarded operation to be refused.
func (p Principal) WithAttribution(a Attribution) Principal {
	p.attribution = a
	p.grants = slices.Clone(p.grants)
	return p
}

// String renders the Principal for display and logging: identifier, Grants and any
// Attribution. The identifier is always rendered, so a record never leaves who acted
// unanswered and an authenticated identity is never confused with an asserted one.
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
