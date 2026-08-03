package authz

import (
	"fmt"
	"slices"
)

// RoleName identifies a Role in the catalogue.
type RoleName string

const (
	// RoleAdmin is one at() rule holding the five resource Actions, extended to
	// every kind beneath its Anchor by prefix domination. It is Kubernetes'
	// resources: '*', verbs: '*' — beneath its Anchor — for free, with no
	// wildcard token anywhere in the rule.
	//
	// Anchored on the root it is the Role that can do everything on every
	// Domain; anchored on one Domain it is that Domain's owner. Same Role, two
	// reaches, which is exactly what naming no kind buys.
	//
	// It deliberately does not hold Attribute. Naming a person is not an
	// administrative power over Kannon's resources; it is the capability of a
	// front-end that has people to name, and an operator administering Kannon
	// directly has nobody to speak for (ADR 0008).
	RoleAdmin RoleName = "admin"

	// RoleSender is what an API Key resolves to: on(batches, create), anchored
	// on the key's own Domain. The rule pins the kind and the Anchor pins the
	// place, so such a key can send for its Domain and do nothing else — it
	// cannot read that Domain, rewrite its Templates, mint a key, or send for
	// anybody else.
	RoleSender RoleName = "sender"
)

// anchorScope is the kind of Anchor a Role's typed rules were written against.
//
// The zero value declares nothing and accepts nothing. That matters because
// concatenating a rule suffix onto the wrong node does not produce an error, it
// produces a different *meaning*: on(templates, ...) anchored at "domains"
// composes "domains/templates", which is the Domain whose FQDN is literally
// "templates". A catalogue entry that forgot to declare its scope must therefore
// refuse every Grant rather than accept them all.
type anchorScope int

const (
	scopeUndeclared anchorScope = iota

	// scopeRoot is for rules naming a kind that lives at the top of the tree —
	// on(domains, ...) composes only there. No seeded Role is root-scoped; the
	// value exists because the vocabulary has three cases and a missing one
	// would be expressed by silence.
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
		// Stated first and explicitly rather than left to the default below: a
		// catalogue entry whose author forgot to declare a scope must refuse
		// every Grant, and the reader of this function should not have to infer
		// that from the absence of a case.
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

// admitsNarrowingTo reports whether a Role of this scope may have its Anchor
// narrowed to that kind by Attenuation.
//
// This is deliberately a second predicate rather than a reuse of accepts, and
// the difference is one case: for a Role of pure shape, accepts defers to
// isGrantable and this does not. Grantability is a constraint on *issuance*
// only. An operator writing a Grant must spell "every Domain" one way, and a
// Grant issued on a path inside a Domain would be an authority nobody could
// reason about from the grants table alone. Attenuation is the opposite
// situation: the authority already exists and is only being reduced, and
// reducing it to a single Template is precisely what lets a front-end scope one
// request to the object that request concerns. Asking accepts here would refuse
// that, so the two questions are asked by two functions rather than by one with
// a flag — a flag would make the wrong answer one missing argument away.
//
// What does *not* differ is the restriction on a typed Role, which is the same
// declaration NewGrant enforces and prevents the same failure: beneath the kind
// its rules were written against, those rules compose paths naming the wrong
// things. sender narrowed to domains/example.com/batches would compose
// domains/example.com/batches/batches — not smaller authority but a different
// and meaningless one. Refusing here is what turns that into a reported drop
// rather than a Grant that silently confers nothing.
func (s anchorScope) admitsNarrowingTo(k anchorKind) bool {
	switch s {
	case scopeAny:
		// Any concrete path the Grant already covers, down to a single item.
		// A malformed Anchor cannot arrive here: Attenuation narrows only to a
		// Resource a Grant covers, and nothing covers a Resource with an empty
		// segment. Were one to arrive anyway it would compose the pattern that
		// covers nothing, so this still fails closed.
		return true
	case scopeRoot:
		// In practice this cannot narrow at all: no Resource is the root, since
		// the root is a flag rather than a path. Stated rather than left out, so
		// that a root-scoped Role — none is seeded — refuses by rule instead of
		// by accident.
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

// childKind names a kind of thing beneath an Anchor, as a slice of segments.
//
// A slice rather than a "stats/aggregated" string, deliberately: a string would
// have to be split on the separator, and splitting a composed path is the one
// operation this layer forbids itself. Rules are authored from the seg constants
// in resource.go, so the segment count is settled at compile time and no input
// reaches it.
type childKind []string

// rule pairs a set of Actions with the suffix — relative to a Grant's Anchor —
// of the things they act on.
//
// Item identifiers never appear in a suffix, because prefix domination already
// reaches them: on(templates, update) composes domains/example.com/templates,
// which dominates every templates/<id> beneath it.
type rule struct {
	actions map[Action]struct{}
	suffix  childKind
}

// at states Actions on the anchored Resource itself and, by domination, on
// everything beneath it.
//
// A Role built only from at() rules names no kind, which is why it fits either
// kind of Anchor: there is nothing in it for an Anchor to be the wrong kind for.
func at(actions ...Action) rule {
	return rule{actions: actionSet(actions...)}
}

// on states Actions on a kind of thing beneath the Anchor.
//
// Multi-segment suffixes are allowed, and one of them is load-bearing:
// on(childKind{segStats, segAggregated}, read) is the counters without the
// per-Delivery rows above them, which is the line between data carrying a
// Recipient address and data carrying none.
func on(k childKind, actions ...Action) rule {
	return rule{actions: actionSet(actions...), suffix: k}
}

// holds reports whether this rule states the Action.
func (r rule) holds(a Action) bool {
	_, ok := r.actions[a]
	return ok
}

// role is a catalogue entry: a name, the kind of Anchor its rules were written
// against, and the rules themselves.
//
// A Role says what may be done and to which kinds of things, never *where* — the
// where is the Anchor of the Grant that places it. Keeping the two apart is what
// lets one issued *name* span kinds while reach stays the Grant's business, so
// the same Role on a wider Anchor reaches further without gaining a single
// Action.
type role struct {
	name RoleName

	// scope is declared rather than derived from the rules. Deriving it would
	// mean that adding one kind-naming rule to a Role silently changed which
	// Grants of it are constructible — a change to every credential in
	// circulation, arriving as a side effect. Declared, it is a line a reviewer
	// reads, and a forgotten declaration refuses every Grant.
	scope anchorScope

	rules []rule
}

// catalogue is the whole of what Roles mean.
//
// It lives in code rather than in the database, so that a Role's meaning is
// settled in one place at review time and a change to it takes effect at once
// for every credential — including ones issued long before, which matters
// because a Principal carries Role *names* and expands them at the moment of the
// check. An expanded snapshot would freeze the semantics of every token already
// in circulation.
//
// It is seeded with exactly the two Roles today's Principal producers can
// resolve to: the API Key adapter and the transition Principal that keeps the
// currently open surfaces working. The wider vocabulary ADR 0008 documents —
// domain-admin, template-editor, key-manager, analyst, metrics-reader, viewer —
// stays documentation until the grants table gives anything a way to issue it. A
// Role nothing can issue would be dead vocabulary, and would churn when that
// table is designed.
//
// The cost is accepted: a Role tailored to one customer requires a deploy. When
// that is genuinely needed, this map can become one implementation of a
// catalogue interface without Principal or Can changing shape.
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

// lookupRole returns the catalogue entry of that name, if there is one.
//
// Unexported because nothing outside this package has any business holding a
// Role: the catalogue is code, so a caller that wants to know what a Role can do
// reads it, and a caller that wants a decision asks Can.
func lookupRole(name RoleName) (role, bool) {
	r, ok := catalogue[name]
	return r, ok
}

// ParseRoleName validates a string against the catalogue.
//
// This is the boundary at which a stored or wire-format name becomes a typed
// one. It refuses an unknown name rather than accepting it to confer nothing
// later, so that a typo in a grants table surfaces where it was made.
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
