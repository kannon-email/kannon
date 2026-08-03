package authz

import "slices"

// Attenuate narrows a Principal to the given concrete Resources, returning the narrowed
// Principal and those it could not narrow to. Intersection rather than substitution, so
// asking for more yields less; only Anchors move, never Roles or the identifier (ADR 0008).
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
			// The Role is resolved first: nothing about a Grant can be judged without
			// knowing what it grants, and a Role the catalogue no longer holds must narrow
			// to nothing, exactly as Can gives it nothing.
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
