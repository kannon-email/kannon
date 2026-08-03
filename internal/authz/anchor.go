package authz

import (
	"strings"

	"github.com/kannon-email/kannon/internal/values"
)

// Anchor is the Resource a Grant fixes its Role to — where the Role's rules
// attach. A Role says what may be done and to which kinds of things; the Anchor
// says where.
//
// An Anchor is *structural* and is never parsed from a composed path string.
// That is the same property Resource has, for the same reason and with more at
// stake: splitting a path on the separator would let an identifier that happens
// to contain one invent a segment, and an Anchor is precisely what bounds reach,
// so an invented segment is an invented authority. Assembling from typed
// constructors makes the number of segments a fact about the call site rather
// than about its input.
//
// The root is represented by an explicit flag rather than by an empty segment
// list. Were "everything" spelled as zero segments, the zero value of an Anchor
// — and of the pattern it composes — would be the widest authority in the
// system instead of the narrowest, so a forgotten assignment would fail open.
// It fails closed instead: a zero Anchor is not grantable and covers nothing.
type Anchor struct {
	root     bool
	segments []patternSegment
}

// RootAnchor is the root, authored "*": the whole tree.
//
// A Grant anchored here confers unbounded *reach*, not unbounded power. The
// Role's rules still bound what may be done and to which kinds of things, so an
// at(read, list) Role on the root sees everything and can change nothing.
func RootAnchor() Anchor {
	return Anchor{root: true}
}

// DomainAnchor is one Domain: domains/<fqdn>.
//
// Everything of that Domain lies beneath it — Templates, Batches, API Keys and
// statistics — because matching is prefix domination and nothing has to be named
// for it to be reached. The FQDN arrives already canonical: this layer compares
// and never normalises, since lower-casing here while two case-differing Domains
// could coexist would itself be the escalation.
func DomainAnchor(f values.DomainName) Anchor {
	return Anchor{segments: []patternSegment{literalSegment(segDomains), literalSegment(f.String())}}
}

// AllDomainsAnchor is every Domain: domains/*, future Domains included.
//
// This is the same *kind* of Anchor as one Domain rather than a wider kind: the
// wildcard stands for exactly one segment, so a Role's typed rules compose
// beneath a Domain either way and neither spelling reaches the domains
// collection above them. What differs is only how many Domains are reached.
//
// The wildcard is also the point: it shouts that the reach is unbounded and
// includes Domains that do not exist yet. That is why "every Domain" has this
// one spelling and the bare "domains" is refused rather than admitted as a
// synonym — two spellings of one authority in a grants table would need an
// equivalence, and the equivalence would be a rewrite inside the one layer that
// must never normalise anything.
func AllDomainsAnchor() Anchor {
	return Anchor{segments: []patternSegment{literalSegment(segDomains), wildcardSegment()}}
}

// AnchorOf is the Anchor at exactly this Resource.
//
// Every segment becomes a *literal*, including one that happens to be an
// asterisk. That is why a wildcard is represented structurally rather than by
// its token: were this conversion textual, a Template identifier of "*" arriving
// from a caller would become a wildcard here, and Attenuation — the one caller
// that turns a requested Resource into an Anchor — could widen authority instead
// of narrowing it.
//
// Most Resources yield an Anchor that NewGrant refuses, and that is a feature
// rather than a limitation: this is the reachable path by which a non-grantable
// Anchor gets attempted and named in the refusal. AnchorOf(Domains()) is the
// bare "domains", whose error suggests "domains/*".
func AnchorOf(r Resource) Anchor {
	segments := make([]patternSegment, 0, len(r.segments))
	for _, s := range r.segments {
		segments = append(segments, literalSegment(s))
	}
	return Anchor{segments: segments}
}

// String renders the Anchor for display, logging and the error messages NewGrant
// returns: "*" for the root, otherwise the path. The zero Anchor renders empty,
// because it names nothing.
//
// It is not a serialisation — nothing parses an Anchor back from it. That is
// what lets a literal asterisk and the wildcard share a token on the way out:
// they are distinguished structurally, and matching never consults this string.
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

// anchorKind classifies an Anchor by the kind of thing it names, which is what a
// Role's typed rules are written against.
//
// The zero value is the non-grantable one. A classification that falls through,
// or an Anchor that was never constructed, therefore refuses a Grant rather than
// being mistaken for the root — which is the one misclassification that would
// hand out everything.
type anchorKind int

const (
	kindOther anchorKind = iota
	kindRoot
	kindDomain
)

// kind classifies this Anchor.
//
// domains/<fqdn> and domains/* are deliberately one kind: both name "a Domain",
// and every rule suffix composes identically beneath either. Nothing else in the
// tree has this shape, and the FQDN dot rule (internal/values) is what keeps it
// that way — a single-label Domain named "templates" would otherwise make
// domains/templates read as both a Domain and a node inside one.
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

// isGrantable reports whether a Grant may be issued on an Anchor of this kind.
//
// Only two kinds are, and everything else is refused rather than left to mean
// something other than what it says. The refusal is not defensiveness: composing
// a rule suffix onto the wrong node does not produce an error, it produces a
// different *meaning*. on(templates, ...) anchored at "domains" composes
// "domains/templates" — not a dead path but the Domain whose FQDN is literally
// "templates", an alias no reader would spot.
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

// namesDomainsCollection reports whether this is the bare domains collection.
//
// It is singled out because it is the one non-grantable Anchor with an obvious
// intended meaning: it is what an author reaches for to say "every Domain", so
// its refusal can name the alternative instead of merely listing the two
// grantable kinds.
func (a Anchor) namesDomainsCollection() bool {
	return !a.root && len(a.segments) == 1 && a.segments[0].isLiteral(segDomains)
}

// isWellFormed reports whether every segment names something.
//
// A zero values.DomainName or a blank identifier reaching a constructor produces an
// Anchor with an empty segment. Such an Anchor must cover nothing, rather than
// fall back on whatever the shorter path it would otherwise compose happens to
// reach.
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

// extend composes the pattern a rule reaches under this Anchor: the Anchor's
// segments followed by the rule's suffix.
//
// The composition is structural — segments appended to segments, never strings
// joined and split back apart — so a suffix cannot introduce a separator and an
// identifier cannot introduce a segment. Suffix segments are always literals:
// only AllDomainsAnchor puts a wildcard into a pattern, and it does so
// structurally.
//
// A zero or malformed Anchor yields the pattern that covers nothing, whatever
// the suffix, and the guard is explicit because the failure it prevents is not
// an empty result. Without it, an Anchor with no segments plus the suffix of
// on(batches, create) would compose the pattern "batches" — not the empty
// authority the caller believes it has, but a different and unrelated one.
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

// covers reports whether the Anchor's own reach includes r, ignoring any rule
// suffix.
//
// This is the question Attenuation asks — a Grant can only be narrowed to
// somewhere it already reaches — and it is answered by the same prefix
// domination that answers authorization requests, so there is one matcher in the
// system rather than a second one deciding inclusion.
func (a Anchor) covers(r Resource) bool {
	return a.extend(nil).covers(r)
}
