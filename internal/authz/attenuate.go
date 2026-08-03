package authz

import "slices"

// Attenuate narrows a Principal to the given Resources, returning the narrowed
// Principal and the requested Resources it could not narrow to.
//
// This is the mechanism a front-end uses to act with one of its users' reach
// rather than its own. It is *not* impersonation: in Kubernetes and GCP that
// word means acquiring another principal's authority, which may be greater than
// or simply different from one's own, whereas here authority can only shrink and
// never moves sideways. ADR 0008 records the distinction.
//
// No Action is required to call this. Giving up authority one already holds is
// always safe, so gating it would buy nothing.
//
// Widening is not a mistake that can be made, because the result is an
// *intersection* rather than a validated substitution. Under substitution the
// subset check would be the only thing standing between narrowing and privilege
// escalation — and therefore a check that could be forgotten. Here a request for
// more than the Principal holds yields less rather than more: the failure mode
// is empty authority, which fails closed.
//
// The Resources must be concrete, which is what keeps this cheap: "is this
// covered?" is answered by the same matcher that answers authorization
// requests, so there is one piece of matching logic in the system rather than a
// second one deciding inclusion between patterns. Prefix domination is what
// makes concrete paths sufficient — narrowing to a Domain still reaches that
// Domain's Templates. Concreteness is structural rather than validated: a
// Resource is assembled from typed constructors and AnchorOf turns every segment
// into a literal, so a segment that happens to be an asterisk narrows to the one
// object of that name instead of widening to all of its siblings.
//
// What narrows is each Grant's Anchor. The Role's rules travel unchanged, so
// narrowing where authority lands never changes what it is, and one old hazard
// is inexpressible: no Anchor names a kind, so no attenuation can trade one
// kind's authority for another's.
//
// Typed rules impose one restriction of their own. A Role does not narrow
// beneath the Anchor kind its rules were written against, because beneath it
// those rules compose paths that name the wrong things: sender narrowed to
// domains/example.com/batches would compose domains/example.com/batches/batches.
// That is not smaller authority but a different and meaningless one, and the
// check is the same Anchor-kind declaration NewGrant enforces — asked here
// through admitsNarrowingTo, since grantability itself binds only at issuance
// (see role.go). A Role of pure shape narrows to any concrete path it covers,
// down to a single Template, which is what lets a front-end scope one request to
// exactly the object it concerns.
//
// The identifier is preserved. Narrowing changes what may be done and never who
// did it; were the identity to change too, the recorded actor would sometimes be
// authenticated and sometimes asserted, with nothing to tell them apart.
//
// Any Attribution already claimed is carried through unchanged, since narrowing
// reach says nothing about who asked.
//
// The second return value lists the requested Resources that yielded no Grant —
// covered by none, or refused by the kind restriction. Silent narrowing is safe
// but hides mistakes: without this, a front-end with a typo in a path would see
// its user refused and have no way to learn it had asked for something it does
// not hold. The kind restriction is the case that most needs reporting, because
// the Grant it declines to produce would have conferred nothing while looking
// like authority in every log line that rendered it.
//
// Three decisions the shape of this loop settles:
//
//   - Asking for nothing yields nothing. Attenuate() with no Resources returns a
//     Principal that can do nothing, rather than one left unnarrowed. A caller
//     that computed an empty list of paths and expected full authority back has a
//     bug, and it fails closed.
//   - A repeated request is one request. The same Resource named twice narrows
//     once and, if it yields nothing, is reported once. Duplicate Grants would
//     add nothing to the union that is a Principal's authority while adding a
//     second identical line to every record that renders it, which a reader would
//     reasonably read as meaning something.
//   - Two Grants of the *same* Role covering one path collapse for the same
//     reason; two Grants of *different* Roles do not, because their union is
//     precisely what the Principal held there.
//
// A narrowed Grant is a runtime value and never an issuance. Its Anchor may be a
// path NewGrant would refuse, so putting one back through NewGrant — or into a
// grants table — would rightly fail. That asymmetry is the point rather than an
// oversight: what an operator may write down and what a request may reduce
// itself to are two different questions.
func (p Principal) Attenuate(resources ...Resource) (Principal, []Resource) {
	narrowed := make([]Grant, 0, len(resources))
	var dropped []Resource
	var asked []Resource

	for _, r := range resources {
		if slices.ContainsFunc(asked, r.Equal) {
			continue
		}
		asked = append(asked, r)

		anchor := AnchorOf(r)
		kind := anchor.kind()

		var taken []RoleName
		for _, g := range p.grants {
			if slices.Contains(taken, g.role) {
				continue
			}
			// The Role is resolved first: nothing about a Grant can be judged
			// without knowing what it grants, and a Role the catalogue no longer
			// holds confers nothing — so narrowing it must yield nothing too,
			// exactly as Can gives it nothing.
			def, ok := lookupRole(g.role)
			if !ok {
				continue
			}
			// Intersection: a Grant contributes only where it already reaches.
			if !g.anchor.covers(r) {
				continue
			}
			if !def.scope.admitsNarrowingTo(kind) {
				continue
			}
			taken = append(taken, g.role)
			narrowed = append(narrowed, Grant{role: g.role, anchor: anchor})
		}

		if len(taken) == 0 {
			dropped = append(dropped, r)
		}
	}

	return Principal{
		id:          p.id,
		grants:      narrowed,
		attribution: p.attribution,
	}, dropped
}
