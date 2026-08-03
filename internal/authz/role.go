package authz

import (
	"fmt"
	"slices"
)

// RoleName identifies a Role in the catalogue.
type RoleName string

const (
	// RoleAdmin is one at() rule holding the five resource Actions, extended to every
	// kind beneath its Anchor by prefix domination — so the same Role is everything on
	// the root and one Domain's owner on a Domain. It holds no Attribute (ADR 0008).
	RoleAdmin RoleName = "admin"

	// RoleSender is what an API Key resolves to: on(batches, create), anchored on the
	// key's own Domain. The rule pins the kind and the Anchor the place, so such a key
	// can only send for its Domain — not read it, rewrite its Templates or mint a key.
	RoleSender RoleName = "sender"
)

// anchorScope is the kind of Anchor a Role's typed rules were written against. The zero
// value declares nothing and accepts nothing: composing a rule suffix onto the wrong node
// yields a different meaning rather than an error, so a missing declaration must refuse.
type anchorScope int

const (
	scopeUndeclared anchorScope = iota

	// scopeRoot is for rules naming a kind at the top of the tree — on(domains, ...)
	// composes only there. No seeded Role is root-scoped; the value exists so that the
	// third case of the vocabulary is not expressed by silence.
	scopeRoot

	// scopeDomain is for rules naming a kind beneath a Domain: templates,
	// apikeys, batches, stats.
	scopeDomain

	// scopeAny is for a Role of pure shape — at() rules only, naming no kind —
	// so there is nothing for an Anchor's kind to disagree with. Such a Role is
	// meaningful anywhere.
	scopeAny
)

// accepts reports whether a Role of this scope may be anchored on that kind.
func (s anchorScope) accepts(k anchorKind) bool {
	if s == scopeUndeclared {
		// Stated explicitly rather than left to the default below: a catalogue entry whose
		// author forgot to declare a scope must refuse every Grant, and a reader should not
		// have to infer that from an absent case.
		return false
	}

	switch s {
	case scopeAny:
		return k.isGrantable()
	case scopeRoot:
		return k == kindRoot
	case scopeDomain:
		return k == kindDomain
	default:
		return false
	}
}

// admitsNarrowingTo reports whether a Role of this scope may have its Anchor narrowed to
// that kind. Apart from accepts because grantability constrains issuance only, while
// narrowing reduces existing authority; a typed Role is still refused off its own kind.
func (s anchorScope) admitsNarrowingTo(k anchorKind) bool {
	switch s {
	case scopeAny:
		// Any concrete path the Grant already covers, down to a single item. A malformed
		// Anchor cannot arrive here — nothing covers a Resource with an empty segment —
		// and were one to, it would compose the pattern that covers nothing.
		return true
	case scopeRoot:
		// In practice this cannot narrow at all: no Resource is the root, which is a flag
		// rather than a path. Stated so that a root-scoped Role — none is seeded — refuses
		// by rule instead of by accident.
		return k == kindRoot
	case scopeDomain:
		return k == kindDomain
	default:
		// scopeUndeclared, and whatever a later case forgets: a Role whose
		// author did not declare a scope narrows nowhere, for the same reason it
		// is grantable nowhere.
		return false
	}
}

// describe names the scope inside an error a human has to act on.
func (s anchorScope) describe() string {
	switch s {
	case scopeRoot:
		return "root-scoped"
	case scopeDomain:
		return "domain-scoped"
	case scopeAny:
		return "anchor-agnostic"
	default:
		return "missing its Anchor scope declaration"
	}
}

// childKind names a kind of thing beneath an Anchor, as segments rather than a
// "stats/aggregated" string: splitting a composed path is the one operation this layer
// forbids itself. Rules are authored from the seg constants, so no input reaches it.
type childKind []string

// rule pairs a set of Actions with the suffix — relative to a Grant's Anchor — of the
// things they act on. Item identifiers never appear in a suffix: on(templates, update)
// composes .../templates, which already dominates every templates/<id> beneath it.
type rule struct {
	actions map[Action]struct{}
	suffix  childKind
}

// at states Actions on the anchored Resource itself and, by domination, on everything
// beneath it. A Role built only from at() rules names no kind, which is why it fits
// either kind of Anchor: there is nothing in it for an Anchor to be the wrong kind for.
func at(actions ...Action) rule {
	return rule{actions: actionSet(actions...)}
}

// on states Actions on a kind of thing beneath the Anchor. Multi-segment suffixes are allowed and
// one is load-bearing: on(stats/aggregated, read) is the counters without the per-Delivery rows
// above them — the line between data carrying an address and data carrying none.
func on(k childKind, actions ...Action) rule {
	return rule{actions: actionSet(actions...), suffix: k}
}

// holds reports whether this rule states the Action.
func (r rule) holds(a Action) bool {
	_, ok := r.actions[a]
	return ok
}

// role is a catalogue entry: a name, the kind of Anchor its rules were written against,
// and the rules. A Role says what may be done and to which kinds of things, never where —
// that is its Grant's Anchor, so a wider Anchor reaches further without a new Action.
type role struct {
	name RoleName

	// scope is declared rather than derived from the rules: deriving it would let one added
	// rule silently change which Grants of this Role are constructible — a change to every
	// credential in circulation, arriving as a side effect.
	scope anchorScope

	rules []rule
}

// catalogue is the whole of what Roles mean. In code rather than in the database, so a
// Role's meaning is settled at review time and applies at once to credentials issued long
// before. Seeded with the two Roles anything can issue today; the rest is ADR 0008's.
var catalogue = buildCatalogue(
	role{
		name:  RoleAdmin,
		scope: scopeAny,
		rules: []rule{at(resourceActions...)},
	},
	role{
		name:  RoleSender,
		scope: scopeDomain,
		rules: []rule{on(childKind{segBatches}, Create)},
	},
)

func buildCatalogue(roles ...role) map[RoleName]role {
	m := make(map[RoleName]role, len(roles))
	for _, r := range roles {
		m[r.name] = r
	}
	return m
}

func actionSet(actions ...Action) map[Action]struct{} {
	m := make(map[Action]struct{}, len(actions))
	for _, a := range actions {
		m[a] = struct{}{}
	}
	return m
}

// lookupRole returns the catalogue entry of that name, if there is one. Unexported: the
// catalogue is code, so a caller that wants to know what a Role can do reads it, and one
// that wants a decision asks Can.
func lookupRole(name RoleName) (role, bool) {
	r, ok := catalogue[name]
	return r, ok
}

// ParseRoleName validates a string against the catalogue — the boundary at which a stored
// or wire name becomes a typed one. An unknown name is refused rather than left to confer
// nothing later, so a typo in a grants table surfaces where it was made.
func ParseRoleName(s string) (RoleName, error) {
	if _, ok := catalogue[RoleName(s)]; !ok {
		return "", fmt.Errorf("unknown role %q", s)
	}
	return RoleName(s), nil
}

// RoleNames lists the catalogue, sorted, for display and for the tests that hold
// its shape to what ADR 0008 seeds.
func RoleNames() []RoleName {
	names := make([]RoleName, 0, len(catalogue))
	for name := range catalogue {
		names = append(names, name)
	}
	slices.Sort(names)
	return names
}
