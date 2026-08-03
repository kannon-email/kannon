// Package authz decides what a request is allowed to do.
//
// The model is recorded in ADR 0008 and its language in CONTEXT.md §Access
// control. In short: a Principal carries Grants, each a Role *name* fixed to an
// Anchor; a Role is a named set of typed rules — Actions paired with the kind of
// thing they act on — defined in this package's catalogue rather than in the
// database; a Resource is a hierarchical path, and authority over a path extends
// to everything beneath it. Authority is the union of a Principal's Grants and
// nothing ever subtracts from it: there are no deny rules.
//
// Four properties shape the code here and are worth stating before reading it.
//
// Reach and power are independent, and the two halves live in different types. A
// Role's rules bound what may be done and to which kinds of things; the Grant's
// Anchor bounds where. So a Grant on the root confers unbounded reach and not
// unbounded power — an at(read, list) Role anchored there sees everything and can
// change nothing — while a powerful Role reaches no further than its Anchor.
//
// Can is a pure function. It takes no context, performs no I/O and consults no
// repository, so the whole decision procedure is testable as a table. A Principal
// reaches a call site through the context, but that is transport: the decision
// never sees it.
//
// Nothing in this package parses a composed path, and nothing normalises
// anything. Resources and Anchors are built from typed constructors and never
// split out of strings, and the FQDN they embed arrives already canonical (see
// internal/values). Both are security properties rather than style preferences.
// Splitting a path would let an identifier carrying a separator invent a segment.
// Normalising would be an escalation rather than a convenience: while two
// case-differing Domains can coexist in the database, lower-casing an FQDN inside
// an authorization decision would hand a Grant on "TEST.com" the other Domain's
// data.
//
// Attenuation and Attribution are two separate mechanisms and ADR 0008 records
// why. Narrowing a Principal needs no Action because it can only shrink
// authority; naming a person needs the attribute Action because it writes an
// unverifiable claim into the record of who did what.
package authz

import "errors"

// Errors returned by Require and Guard. Both should surface as permission
// denied at any edge; they are distinguished so that a log can tell "this
// credential may not do this" from "nothing authenticated this request at all",
// which are very different operational problems.
var (
	ErrForbidden   = errors.New("forbidden")
	ErrNoPrincipal = errors.New("no principal in context")
)

// Can reports whether p may perform a on r.
//
// The Principal carries Role names and the expansion into rules happens here, at
// the moment of the check. A credential may outlive a change to what a Role
// means: an expanded snapshot would freeze the semantics of every token already
// issued, so widening or narrowing a Role would mean finding and reissuing them.
// Carrying the name means one edit to the catalogue takes effect everywhere at
// once.
//
// A Grant satisfies the request when one of its Role's rules holds the Action
// *and* that rule's effective pattern covers r. The effective pattern is the
// Grant's Anchor concatenated with the rule's suffix, composed structurally, and
// it is tested by the same prefix domination that answers every other question
// about reach — there is one matcher in the system.
//
// A Grant naming a Role absent from the catalogue confers nothing. NewGrant
// cannot produce one, but a Role removed from the catalogue while credentials
// naming it are still in circulation can — in which case conferring nothing is
// the right answer.
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
