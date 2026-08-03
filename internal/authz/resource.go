package authz

import (
	"slices"
	"strings"

	"github.com/kannon-email/kannon/internal/values"
)

// The segments of the Resource tree, named here rather than inline: domains, domains/<name>
// (update = SetTrackingPolicy), .../batches (create = SendHTML / SendTemplate),
// .../templates/<id>, .../apikeys/<id>, .../stats (per-Delivery rows), .../stats/aggregated.
const (
	segDomains    = "domains"
	segBatches    = "batches"
	segTemplates  = "templates"
	segAPIKeys    = "apikeys"
	segStats      = "stats"
	segAggregated = "aggregated"

	// separator appears in the display form of a path and nowhere else. It never
	// appears in the construction of a Resource, an Anchor or a pattern, all of
	// which are assembled from segments and never split out of a string.
	separator = "/"

	// wildcardToken is how the every-Domain Anchor and the root are written for
	// a human to read. It is display only: a wildcard is a flag on a segment, so
	// a segment whose text happens to be an asterisk stays a literal.
	wildcardToken = "*"
)

// Resource is what a request acts on, named by a hierarchical path. Always built through the
// constructors below and never parsed from a string: splitting a joined path would let a
// caller's identifier invent segments. An empty segment is covered by nothing, so it fails closed.
type Resource struct {
	segments []string
}

// Domains names the collection of all Domains: create makes one, list
// enumerates them.
func Domains() Resource {
	return Resource{segments: []string{segDomains}}
}

// Domain names one Domain. Authority over it reaches that Domain's Templates,
// Batches, API Keys and statistics, so update here is also update on its
// Templates — see ADR 0008.
func Domain(f values.DomainName) Resource {
	return Resource{segments: []string{segDomains, f.String()}}
}

// Batches names a Domain's Batches. Sending mail is create on this.
func Batches(f values.DomainName) Resource {
	return under(f, segBatches)
}

// Templates names a Domain's Templates as a collection.
func Templates(f values.DomainName) Resource {
	return under(f, segTemplates)
}

// Template names one Template of a Domain.
func Template(f values.DomainName, id string) Resource {
	return under(f, segTemplates, id)
}

// APIKeys names a Domain's API Keys as a collection.
func APIKeys(f values.DomainName) Resource {
	return under(f, segAPIKeys)
}

// APIKey names one API Key of a Domain.
func APIKey(f values.DomainName, id string) Resource {
	return under(f, segAPIKeys, id)
}

// Stats names a Domain's per-Delivery statistics — the rows carrying a
// Recipient address and, under Full, an IP address and user agent.
func Stats(f values.DomainName) Resource {
	return under(f, segStats)
}

// AggregatedStats names a Domain's counters, which carry no personal data. Beneath Stats
// deliberately: authority over the per-Delivery rows implies authority over the counters —
// true anyway — and the incoherent "detail but not aggregate" Grant becomes unwritable.
func AggregatedStats(f values.DomainName) Resource {
	return under(f, segStats, segAggregated)
}

// under builds a path beneath one Domain.
func under(f values.DomainName, tail ...string) Resource {
	segments := make([]string, 0, len(tail)+2)
	segments = append(segments, segDomains, f.String())
	segments = append(segments, tail...)
	return Resource{segments: segments}
}

// String renders the path for display and logging. Not a serialisation: nothing parses a
// Resource back from it, and a segment holding an identifier that contains a separator would
// render ambiguously. Comparison and matching work on segments, never on this.
func (r Resource) String() string {
	return strings.Join(r.segments, separator)
}

// Equal reports whether two Resources name the same thing.
func (r Resource) Equal(other Resource) bool {
	return slices.Equal(r.segments, other.segments)
}

// isWellFormed reports whether every segment is non-empty. A Resource that is
// not well formed is covered by nothing.
func (r Resource) isWellFormed() bool {
	if len(r.segments) == 0 {
		return false
	}
	return !slices.Contains(r.segments, "")
}

// patternSegment is one segment of a pattern: a wildcard matching any single segment, or a
// literal that must match exactly. The wildcard is a flag rather than the token "*" in the
// literal, which keeps a Resource segment that happens to be an asterisk a literal.
type patternSegment struct {
	literal  string
	wildcard bool
}

// literalSegment matches exactly this text and nothing else.
func literalSegment(s string) patternSegment {
	return patternSegment{literal: s}
}

// wildcardSegment matches any one segment. There is no any-depth segment: prefix domination
// already extends a pattern to everything beneath what it names, and an implicit "**" would
// let a future node silently enter the reach of Grants issued years earlier.
func wildcardSegment() patternSegment {
	return patternSegment{literal: wildcardToken, wildcard: true}
}

// isLiteral reports whether this segment is exactly the given text, as a
// literal. A wildcard is never any literal, however it renders.
func (s patternSegment) isLiteral(text string) bool {
	return !s.wildcard && s.literal == text
}

// pattern is the reach of one of a Role's rules under a Grant's Anchor. Composed, never
// authored or parsed — splitting a path would let an identifier invent a segment — so the
// type is unexported and the zero value, the root being a flag, covers nothing.
type pattern struct {
	everything bool
	segments   []patternSegment
}

// covers reports whether authority over this pattern reaches r: prefix domination, the single
// matcher in the system. Two things are therefore inexpressible — holding a path without what
// lies under it, and taking anything back. A pattern longer than r covers nothing.
func (p pattern) covers(r Resource) bool {
	if !r.isWellFormed() {
		return false
	}
	if p.everything {
		return true
	}
	if len(p.segments) == 0 || len(p.segments) > len(r.segments) {
		return false
	}

	for i, seg := range p.segments {
		if seg.wildcard {
			continue
		}
		if seg.literal != r.segments[i] {
			return false
		}
	}
	return true
}
