// Package authz decides what a request is allowed to do: a Principal carries Grants, each a
// Role name fixed to an Anchor, and authority over a path extends to everything beneath it,
// with no deny rules. Can is pure; nothing here parses a path or normalises (ADR 0008).
package authz

import "errors"

// Errors returned by Require and Guard. Both surface as permission denied, but are
// distinguished so that a log can tell "this credential may not do this" from "nothing
// authenticated this request at all", which are very different operational problems.
var (
	ErrForbidden   = errors.New("forbidden")
	ErrNoPrincipal = errors.New("no principal in context")
)

// Can reports whether p may perform a on r. Role names expand into rules here, at the check,
// so one edit to the catalogue takes effect for credentials already issued; a Grant naming a
// Role the catalogue no longer holds confers nothing.
func Can(p Principal, a Action, r Resource) bool {
	for _, g := range p.grants {
		def, ok := lookupRole(g.role)
		if !ok {
			continue
		}
		for _, rl := range def.rules {
			if !rl.holds(a) {
				continue
			}
			if g.anchor.extend(rl.suffix).covers(r) {
				return true
			}
		}
	}
	return false
}

// Require is Can as an error, for call sites that propagate rather than branch.
func Require(p Principal, a Action, r Resource) error {
	if !Can(p, a, r) {
		return ErrForbidden
	}
	return nil
}
