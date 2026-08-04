package authz

import (
	"strings"

	"github.com/kannon-email/kannon/internal/values"
)

// Anchor is the Resource a Grant fixes its Role to — the Role says what may be done, the
// Anchor says where. Structural and never parsed from a path string, so no identifier can
// invent a segment; the root is a flag, so a zero Anchor covers nothing and fails closed.
type Anchor struct {
	root     bool
	segments []patternSegment
}

// RootAnchor is the root, authored "*": the whole tree. A Grant anchored here confers
// unbounded reach, not unbounded power — the Role's rules still bound what may be done,
// so an at(read, list) Role on the root sees everything and can change nothing.
func RootAnchor() Anchor {
	return Anchor{root: true}
}

// DomainAnchor is one Domain: domains/<name>. Everything of that Domain lies beneath it by
// prefix domination, named or not. The name arrives already canonical: this layer compares
// and never normalises, since lower-casing here would itself be the escalation.
func DomainAnchor(f values.DomainName) Anchor {
	return Anchor{segments: []patternSegment{literalSegment(segDomains), literalSegment(f.String())}}
}

// AllDomainsAnchor is every Domain: domains/*, future Domains included. The same kind as
// one Domain — the wildcard stands for exactly one segment — and the only spelling of
// "every Domain": admitting the bare "domains" as a synonym would need a rewrite here.
func AllDomainsAnchor() Anchor {
	return Anchor{segments: []patternSegment{literalSegment(segDomains), wildcardSegment()}}
}

// AnchorOf is the Anchor at exactly this Resource, every segment a literal — including one
// that happens to be an asterisk, so a Template identifier of "*" cannot become a wildcard
// and widen authority. Most Resources yield an Anchor NewGrant refuses, and names.
func AnchorOf(r Resource) Anchor {
	segments := make([]patternSegment, 0, len(r.segments))
	for _, s := range r.segments {
		segments = append(segments, literalSegment(s))
	}
	return Anchor{segments: segments}
}

// String renders the Anchor for display, logging and NewGrant's errors: "*" for the root,
// otherwise the path; the zero Anchor renders empty. Not a serialisation — nothing parses
// an Anchor back, which is why a literal asterisk and the wildcard may share a token.
func (a Anchor) String() string {
	if a.root {
		return wildcardToken
	}
	parts := make([]string, 0, len(a.segments))
	for _, s := range a.segments {
		parts = append(parts, s.literal)
	}
	return strings.Join(parts, separator)
}

// anchorKind classifies an Anchor by the kind of thing it names, which is what a Role's
// typed rules are written against. The zero value is the non-grantable one, so a
// classification that falls through refuses rather than being mistaken for the root.
type anchorKind int

const (
	kindOther anchorKind = iota
	kindRoot
	kindDomain
)

// kind classifies this Anchor. domains/<name> and domains/* are deliberately one kind:
// both name "a Domain" and every rule suffix composes identically beneath either. The dot
// rule on a domain name is what keeps that shape unique (internal/values).
func (a Anchor) kind() anchorKind {
	if a.root {
		return kindRoot
	}
	if !a.isWellFormed() {
		return kindOther
	}
	if len(a.segments) == 2 && a.segments[0].isLiteral(segDomains) {
		return kindDomain
	}
	return kindOther
}

// isGrantable reports whether a Grant may be issued on an Anchor of this kind. Only two
// are: composing a rule suffix onto the wrong node yields a different meaning rather than
// an error — on(templates, ...) at "domains" composes the Domain named "templates".
func (k anchorKind) isGrantable() bool {
	return k == kindRoot || k == kindDomain
}

// describe names the kind inside an error a human has to act on.
func (k anchorKind) describe() string {
	switch k {
	case kindRoot:
		return "the root"
	case kindDomain:
		return "a Domain"
	default:
		return "a non-grantable anchor"
	}
}

// namesDomainsCollection reports whether this is the bare domains collection. Singled out
// because it is the one non-grantable Anchor with an obvious intended meaning, so its
// refusal can name the alternative instead of listing the grantable kinds.
func (a Anchor) namesDomainsCollection() bool {
	return !a.root && len(a.segments) == 1 && a.segments[0].isLiteral(segDomains)
}

// isWellFormed reports whether every segment names something. A zero Name or a blank
// identifier reaching a constructor leaves an empty segment, and such an Anchor must cover
// nothing rather than fall back on the shorter path it would otherwise compose.
func (a Anchor) isWellFormed() bool {
	if len(a.segments) == 0 {
		return false
	}
	for _, s := range a.segments {
		if s.literal == "" {
			return false
		}
	}
	return true
}

// extend composes the pattern a rule reaches under this Anchor: segments appended to
// segments, never strings joined and split, so no suffix can introduce a separator. A zero
// or malformed Anchor yields the pattern covering nothing — not the bare "batches".
func (a Anchor) extend(suffix childKind) pattern {
	if !a.root && !a.isWellFormed() {
		return pattern{}
	}
	if a.root && len(suffix) == 0 {
		// The root's own reach. Stated as the flag rather than as zero segments,
		// so the zero pattern stays the narrowest thing in the system and never
		// becomes the widest.
		return pattern{everything: true}
	}

	segments := make([]patternSegment, 0, len(a.segments)+len(suffix))
	segments = append(segments, a.segments...)
	for _, s := range suffix {
		segments = append(segments, literalSegment(s))
	}
	return pattern{segments: segments}
}

// covers reports whether the Anchor's own reach includes r, ignoring any rule suffix. It
// is Attenuation's question — a Grant narrows only to somewhere it already reaches — and
// is answered by the same prefix domination, so the system has one matcher.
func (a Anchor) covers(r Resource) bool {
	return a.extend(nil).covers(r)
}
