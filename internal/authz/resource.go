package authz

import (
	"slices"
	"strings"

	"github.com/kannon-email/kannon/internal/values"
)

// The segments of the Resource tree. Naming them here rather than inline keeps
// the tree readable in one place:
//
//	domains
//	domains/<fqdn>                      update = SetTrackingPolicy
//	domains/<fqdn>/batches              create = SendHTML / SendTemplate
//	domains/<fqdn>/templates/<id>
//	domains/<fqdn>/apikeys/<id>
//	domains/<fqdn>/stats                per-Delivery rows
//	domains/<fqdn>/stats/aggregated     counters
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

// Resource is what a request acts on, named by a hierarchical path.
//
// A Resource is always built through the constructors below and never parsed
// from a string. That is a security property rather than a style preference: a
// segment carrying a caller-supplied identifier could contain the separator,
// and splitting a joined path would let such an identifier invent segments.
// Assembling structurally makes the number of segments a fact about the call
// site rather than about the input.
//
// A Resource with an empty segment can be covered by nothing at all, so a zero
// values.DomainName or a blank identifier reaching a constructor produces an
// unauthorizable Resource. Programming errors on this path therefore fail
// closed rather than falling back on whatever a shorter path would have
// matched.
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

// AggregatedStats names a Domain's counters, which carry no personal data.
//
// It sits *beneath* Stats deliberately: authority over the per-Delivery rows
// implies authority over the counters, which is semantically true — anyone who
// can read every event can count them — and the nesting makes the incoherent
// Grant "detail but not aggregate" impossible to write rather than writable and
// unenforceable.
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

// String renders the path for display and logging.
//
// It is not a serialisation: a Resource is never parsed back from it, and a
// segment holding a caller-supplied identifier that contains a separator would
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

// patternSegment is one segment of a pattern: either a wildcard matching any
// single segment, or a literal that must match exactly.
//
// The wildcard is a flag rather than the token "*" sitting in the literal, which
// is what keeps a Resource segment that happens to be an asterisk a literal. A
// wildcard still carries the token in its literal, but only so that String can
// render it.
type patternSegment struct {
	literal  string
	wildcard bool
}

// literalSegment matches exactly this text and nothing else.
func literalSegment(s string) patternSegment {
	return patternSegment{literal: s}
}

// wildcardSegment matches any one segment. There is no segment matching any
// depth: prefix domination already extends every pattern to everything beneath
// what it names, so a trailing wildcard would be redundant, and an implicit "**"
// in the middle would mean a future node silently entered the reach of Grants
// issued years earlier.
func wildcardSegment() patternSegment {
	return patternSegment{literal: wildcardToken, wildcard: true}
}

// isLiteral reports whether this segment is exactly the given text, as a
// literal. A wildcard is never any literal, however it renders.
func (s patternSegment) isLiteral(text string) bool {
	return !s.wildcard && s.literal == text
}

// pattern is the reach of one of a Role's rules under a Grant's Anchor: the set
// of Resources that rule matches.
//
// A pattern is *composed*, never authored, which is why the type is unexported
// and why nothing here parses one from a string. Parsing a composed path is the
// one operation this layer forbids itself: splitting on the separator would let
// an identifier carrying one invent a segment, and the whole model rests on a
// Domain having exactly one spelling. Grants therefore carry an Anchor, and a
// pattern only ever appears between Anchor and matcher.
//
// The root's reach is the everything flag rather than zero segments, so the zero
// value covers nothing and a pattern that was never composed fails closed.
type pattern struct {
	everything bool
	segments   []patternSegment
}

// covers reports whether authority over this pattern reaches r.
//
// This is prefix domination, and it is the single matcher in the system: the
// pattern matches when it equals r or is *a prefix of* r, so a Grant on
// domains/example.com reaches that Domain's Templates and statistics without
// naming them. Two things are therefore inexpressible by design — holding a path
// without what lies under it, and taking anything back.
//
// Note the asymmetry that makes Attenuation cheap: a pattern longer than r
// covers nothing, so domains/*/apikeys does not cover domains/example.com. That
// is the right answer, since granting the latter would also reach that Domain's
// Templates, which the former does not hold.
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
