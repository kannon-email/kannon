// Package tracking owns the Tracking Mode / Tracking Policy vocabulary defined
// in CONTEXT.md: how much a single engagement channel (opens, links) may be
// observed about a Delivery, and how the statements made at Domain, Batch and
// Recipient level collapse into the one Policy that governs it.
//
// The package sits below the batch, delivery and envelope modules so all three
// can depend on it without a cycle, and it deliberately depends on nothing —
// in particular not on the protobuf packages. Translation to and from the wire
// enums belongs at the API boundary.
package tracking

// Mode is how much one engagement channel may be observed. The values are
// lowercase strings, matching the convention of the other persisted enums
// (SendingPoolStatus, StatsType).
//
// The zero value is the empty string and means "states nothing", which makes
// the Go zero value, an absent JSON key and the protobuf3 zero value coincide
// with no glue code.
type Mode string

const (
	// ModeUnspecified states nothing, and so imposes no restriction of its own.
	ModeUnspecified Mode = ""
	// ModeOff means the channel is not observed at all.
	ModeOff Mode = "off"
	// ModeAnonymous means the channel is counted in aggregate only; nothing is
	// retained that could isolate one Recipient from another.
	ModeAnonymous Mode = "anonymous"
	// ModePseudonymous means events are linkable within a single Batch but
	// carry no Recipient identity: they name a pseudonym drawn fresh for every
	// Delivery of every Batch (see identity.go).
	ModePseudonymous Mode = "pseudonymous"
	// ModeIdentified means events are attributed to the Recipient.
	ModeIdentified Mode = "identified"
	// ModeFull means events are attributed, and the IP address and user agent
	// of the request are retained.
	ModeFull Mode = "full"
)

// modeRanks fixes the order of the scale, by increasing collection. It lives
// here as plain Go and is deliberately not derived from the protobuf enum
// numbers, so a future Mode can be inserted mid-scale without renumbering the
// wire. ModeUnspecified is absent on purpose: it states nothing, so it has no
// position on the scale.
var modeRanks = map[Mode]int{
	ModeOff:          0,
	ModeAnonymous:    1,
	ModePseudonymous: 2,
	ModeIdentified:   3,
	ModeFull:         4,
}

// Rank returns m's position on the scale, where a lower rank collects less, so
// that comparing two Modes is a rank comparison rather than a tree of special
// cases. It reports false for a Mode with no position on the scale:
// ModeUnspecified, and any value this build does not know.
func (m Mode) Rank() (int, bool) {
	rank, ok := modeRanks[m]
	return rank, ok
}

// states reports whether m says anything at all. Only ModeUnspecified says
// nothing; every other value is a statement, including one this build cannot
// read.
func (m Mode) states() bool { return m != ModeUnspecified }

// collection returns the Mode this build will actually act on for m, and its
// rank. A stated Mode this build cannot read collapses to ModeOff: it must
// never be confused with silence, because silence defers to the level above and
// would therefore let an unreadable value *widen* what is collected. The
// Domain's Policy is the only guarantee an operator has (ADR 0003), so a Domain
// row written by a newer build has to keep enforcing something rather than
// dissolving into no ceiling at all.
func (m Mode) collection() (Mode, int) {
	if rank, ok := m.Rank(); ok {
		return m, rank
	}
	return ModeOff, modeRanks[ModeOff]
}

// IdentifiesRecipient reports whether an engagement event governed by m may name
// the Recipient it came from. Like every other question about the scale it is a
// rank comparison: Identified is by definition the rung at which attribution
// begins, so every rung below it — Off, Anonymous, Pseudonymous — retains nothing
// that names a Recipient (CONTEXT.md).
//
// ModeUnspecified identifies. It states nothing and so imposes no restriction of
// its own (ADR 0003), and the only place an unstated Mode is still met is a token
// minted before the Mode became a claim: such a token was minted to be
// attributed, so dropping its identity would lose data rather than protect
// anybody.
//
// A Mode that states something this build cannot read does *not* identify. It
// can only come from a token minted by a newer build, whose Mode may well be
// more restrictive than Identified, and reading an unreadable statement as
// permission to attribute would be the one direction that cannot be undone (see
// Mode.collection).
func (m Mode) IdentifiesRecipient() bool {
	if !m.states() {
		return true
	}
	_, rank := m.collection()
	return rank >= modeRanks[ModeIdentified]
}

// IsolatesRecipient reports whether what is retained about an engagement governed
// by m may tell one Recipient of a Batch from another. Pseudonymous is by
// definition the rung at which that begins — its events name nobody yet are
// linkable to each other within a Batch — and below it "nothing is retained that
// could isolate one Recipient from another" (CONTEXT.md).
//
// It is the coarser of the two questions about identity, and answers three that
// would otherwise be asked separately and could drift apart: whether the mint
// draws a per-Delivery identity, whether the resulting token may be shared across
// the Batch, and whether the Tracker keeps the identity the token claims. All
// three turn on the same line, because a token that can isolate a Recipient is by
// construction not the same token for every Recipient.
//
// ModeUnspecified isolates and an unreadable Mode does not, for the reasons given
// on IdentifiesRecipient — this is the same rank comparison one rung lower.
func (m Mode) IsolatesRecipient() bool {
	if !m.states() {
		return true
	}
	_, rank := m.collection()
	return rank >= modeRanks[ModePseudonymous]
}

// Policy is a pair of Modes, one governing opens and one governing links,
// expressing what may be observed about a Delivery.
type Policy struct {
	Opens Mode `json:"opens,omitempty"`
	Links Mode `json:"links,omitempty"`
}

// Resolve collapses the Policies stated at Domain, Batch and Recipient level
// into the single effective Policy governing a Delivery.
//
// A lower level may only restrict what the level above allows, never widen it:
// per axis the result is the most restrictive of the stated Modes, whichever
// level stated it. A level that states nothing imposes no restriction of its
// own, and when no level states anything the result is ModeOff.
func Resolve(domain, batch, recipient Policy) Policy {
	return Policy{
		Opens: mostRestrictive(domain.Opens, batch.Opens, recipient.Opens),
		Links: mostRestrictive(domain.Links, batch.Links, recipient.Links),
	}
}

// Normalized returns p with every Mode that states nothing replaced by
// ModeOff, so that a Policy at rest is always self-describing. It is the same
// rule as Resolve applied to a single level.
func (p Policy) Normalized() Policy {
	return Resolve(p, Policy{}, Policy{})
}

// mostRestrictive returns the lowest-ranked of the stated modes, skipping
// those that state nothing, and ModeOff when none states anything. A stated
// Mode this build cannot read collects nothing (see Mode.collection), so it
// resolves to ModeOff rather than deferring to another level.
func mostRestrictive(modes ...Mode) Mode {
	out := ModeOff
	outRank, found := 0, false
	for _, m := range modes {
		if !m.states() {
			continue
		}
		mode, rank := m.collection()
		if !found || rank < outRank {
			out, outRank, found = mode, rank, true
		}
	}
	return out
}
