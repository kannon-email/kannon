package authz

import "fmt"

// Grant is a Role fixed to an Anchor — *this* Role, anchored *here* — and the unit an
// operator issues: one name in one place, rather than four grants hand-assembled per
// issuance. A Principal's authority is the union of its Grants, and nothing ever subtracts.
type Grant struct {
	role   RoleName
	anchor Anchor
}

// NewGrant fixes a Role to an Anchor, checking the Role, then the Anchor's grantability, then
// its kind — the order that yields the most useful message. Refusing here rather than at a
// check is the point: a Grant composing onto the wrong node means something else, silently.
func NewGrant(role RoleName, anchor Anchor) (Grant, error) {
	def, ok := lookupRole(role)
	if !ok {
		return Grant{}, fmt.Errorf("unknown role %q", role)
	}

	kind := anchor.kind()
	if !kind.isGrantable() {
		if anchor.namesDomainsCollection() {
			return Grant{}, fmt.Errorf("anchor %q is not grantable; did you mean %q?",
				anchor.String(), AllDomainsAnchor().String())
		}
		return Grant{}, fmt.Errorf("anchor %q is not grantable; a Grant is issued on the root (%q) or on a Domain (%q or %q)",
			anchor.String(), RootAnchor().String(), "domains/<name>", AllDomainsAnchor().String())
	}

	if !def.scope.accepts(kind) {
		return Grant{}, fmt.Errorf("role %q is %s and cannot be anchored on %s (%q)",
			role, def.scope.describe(), kind.describe(), anchor.String())
	}

	return Grant{role: role, anchor: anchor}, nil
}

// MustNewGrant is NewGrant for package-level values and tests, where a bad Grant
// is a programming error rather than input.
func MustNewGrant(role RoleName, anchor Anchor) Grant {
	g, err := NewGrant(role, anchor)
	if err != nil {
		panic(err)
	}
	return g
}

// Role returns the granted Role's name — a name and not an expanded Role, since the
// expansion happens at the moment of a check, so that a change to what a Role means
// reaches credentials issued long before it.
func (g Grant) Role() RoleName {
	return g.role
}

// Anchor returns where the Role is anchored.
func (g Grant) Anchor() Anchor {
	return g.anchor
}

// String renders the Grant for display and logging: "sender on
// domains/example.com", "admin on *".
func (g Grant) String() string {
	return string(g.role) + " on " + g.anchor.String()
}
