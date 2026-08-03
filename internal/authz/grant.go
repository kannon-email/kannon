package authz

import "fmt"

// Grant is a Role fixed to an Anchor — *this* Role, anchored *here*.
//
// It is the unit an operator issues, and it is deliberately one name in one
// place. "Domain administration" spans Templates, API Keys, statistics and the
// Tracking Policy; a model in which that is four separately attached grants
// turns every issuance into hand-assembly, and an assembly mistake is a silent
// misconfiguration that surfaces only when the fourth operation is attempted.
//
// A Principal carries a set of Grants and its authority is their union. Nothing
// ever removes from that union: "everything except one Domain" is inexpressible,
// deliberately. Deny rules would buy that one phrase and cost a precedence order
// between allow and deny, which is where authorization models stop being readable
// by inspection and where their worst bugs live.
type Grant struct {
	role   RoleName
	anchor Anchor
}

// NewGrant fixes a Role to an Anchor.
//
// The three checks run in the order that yields the most useful message. An
// unknown Role comes first, because nothing else can be judged without knowing
// what was meant. Then whether the Anchor is grantable at all — the bare domains
// collection being singled out, since it is what an author reaches for to say
// "every Domain". Then whether the Anchor's kind is the one the Role's rules were
// written against.
//
// Refusing here rather than at the moment of a check is the point: a Grant whose
// rules would compose onto the wrong node does not fail, it means something else,
// so it cannot be allowed to exist and be discovered by its effects.
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
			anchor.String(), RootAnchor().String(), "domains/<fqdn>", AllDomainsAnchor().String())
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

// Role returns the granted Role's name.
//
// A name and not an expanded Role: the expansion happens at the moment of a
// check, so that a change to what a Role means reaches credentials issued long
// before it.
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
