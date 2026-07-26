package tracking

// Axis identifies one channel of a Tracking Policy — opens or links — so a
// ceiling violation can name which one it concerns.
type Axis string

const (
	// AxisOpens is the open-tracking channel of a Policy.
	AxisOpens Axis = "opens"
	// AxisLinks is the link-tracking channel of a Policy.
	AxisLinks Axis = "links"
)

// CeilingViolation reports that a stated Mode asks for more than a ceiling
// permits on one Axis, carrying enough information for a caller to build an
// error message: which Axis, what the ceiling allows, and what was asked for.
type CeilingViolation struct {
	Axis    Axis
	Ceiling Mode
	Stated  Mode
}

// CeilingViolations compares a stated Policy against the Policy stated at the
// level immediately above it in the cascade — its ceiling — and reports every
// Axis where the stated Mode collects more than the ceiling allows (ADR 0003:
// "a lower level may only restrict, never widen").
//
// A Mode that states nothing never violates, on either side: a ceiling that
// states nothing has nothing to enforce, and a level that states nothing asks
// for nothing above it. The two axes are evaluated independently, so the
// result may report zero, one, or both.
//
// This is the pure half of the ceiling rule; it makes no judgement about what
// a violation means. The Mailer API fails the whole call when the Batch
// violates its Domain's ceiling (#417), and rejects a single Recipient for
// the same reason (#419) — same function, same shape, different caller.
func CeilingViolations(ceiling, stated Policy) []CeilingViolation {
	var violations []CeilingViolation
	if v, ok := axisViolation(AxisOpens, ceiling.Opens, stated.Opens); ok {
		violations = append(violations, v)
	}
	if v, ok := axisViolation(AxisLinks, ceiling.Links, stated.Links); ok {
		violations = append(violations, v)
	}
	return violations
}

// axisViolation reports whether stated collects more than ceiling on one Axis.
// Either side stating nothing short-circuits to "no violation": a ceiling that
// states nothing has nothing to enforce, and a level that states nothing asks
// for nothing above it.
//
// A stated Mode this build cannot read is compared as ModeOff on both sides
// (see Mode.collection), so an unreadable ceiling still enforces the floor
// rather than permitting everything. The violation reports the Modes as they
// were actually configured, not as they were read, so an operator sees the
// value that needs fixing.
func axisViolation(axis Axis, ceiling, stated Mode) (CeilingViolation, bool) {
	if !stated.states() || !ceiling.states() {
		return CeilingViolation{}, false
	}
	_, statedRank := stated.collection()
	_, ceilingRank := ceiling.collection()
	if statedRank <= ceilingRank {
		return CeilingViolation{}, false
	}
	return CeilingViolation{Axis: axis, Ceiling: ceiling, Stated: stated}, true
}
