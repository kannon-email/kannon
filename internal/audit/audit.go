// Package audit is the register of authorization decisions: one Audit Record per decision Guard
// reaches, published to NATS by the process that reached it and written to a table by a worker of
// its own (ADR 0010). Off unless an operator turns it on, and never read back by Kannon — nothing
// here queries, because an authorization decision that could be influenced by the register
// describing it would no longer be a decision about authority.
package audit

import (
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/kannon-email/kannon/internal/authz"
	"github.com/kannon-email/kannon/internal/utils"
)

// idPrefix names what an identifier identifies, as every other identifier in the repository does
// (internal/utils.NewID): audit_<cuid2>.
const idPrefix = "audit"

// Record is one authorization decision written down. Its identifier and its instant both come from
// the producer: the instant so that a worker catching up after an outage does not date a week of
// records to the moment it recovered, and the identifier so that a redelivered message inserts
// nothing the second time — while two genuinely simultaneous identical operations stay two rows,
// which any natural key would collapse into one.
//
// The JSON tags are the wire form and the storage form at once, deliberately: a Record crosses NATS
// as JSON and its Details land in a jsonb column, so one shape end to end means no mapping to keep
// aligned and no migration to add to what is recorded. The Details field is tagged "data" because
// that is what the column and the payload key are called; the Go name says what it holds.
//
// It has NewRecord and no Load, where every persistent entity in the repository has both
// (docs/REPOSITORY_GUIDE.md). Unmarshal is the Load: a Record is reconstructed from the wire and never
// from a row, since nothing reads this table back — so the constructor that validates one is the one
// that parses the payload, and a second entry point would be a second place the invariants live.
type Record struct {
	ID         string        `json:"id"`
	OccurredAt time.Time     `json:"occurred_at"`
	Action     authz.Action  `json:"action"`
	Outcome    authz.Outcome `json:"outcome"`

	// Principal is the identifier of the credential that acted — the credential and not a
	// person, which is what a shared Admin Token makes of it until per-operator credentials
	// land (ADR 0009). Empty exactly when Outcome is no-principal.
	Principal string `json:"principal"`

	// Resource is the path as its segments, which is what the model compares on. Not the joined
	// form: a Resource's own type calls that display only, and a prefix query over it would
	// match a differently-named Domain.
	Resource []string `json:"resource"`

	Details Details `json:"data"`
}

// Details is everything an Audit Record holds beyond its columns. Its own object because it lands
// whole in one jsonb column: what is worth recording will grow, and growing it must not cost a
// migration every time.
type Details struct {
	// Attribution is the person a request claimed to be asking for, when it claimed one. Held
	// beside the credential and never instead of it: one of the two was checked and the other
	// cannot be. This is the personal data an Audit Record carries, and the reason its retention
	// is the operator's to configure.
	Attribution authz.Attribution `json:"attribution,omitempty"`

	// Grants is the authority the Principal held, rendered. Recorded for a refusal because
	// "denied" alone says that and not why, and for a permitted operation because it is equally
	// the reason it was permitted. It names Roles and Anchors, so it carries no personal data.
	Grants []string `json:"grants,omitempty"`

	// Reason names which check refused, empty when none did.
	Reason string `json:"reason,omitempty"`
}

// NewRecord writes a decision down. The identifier is generated here, at the producer, which is what
// makes a redelivery a no-op rather than a second row.
func NewRecord(d authz.Decision) Record {
	return Record{
		ID:         utils.NewID(idPrefix),
		OccurredAt: d.At,
		Action:     d.Action,
		Outcome:    d.Outcome,
		Principal:  d.Principal.ID(),
		Resource:   d.Resource.Segments(),
		Details: Details{
			Attribution: d.Principal.Attribution(),
			Grants:      grantsOf(d.Principal),
			Reason:      d.Reason,
		},
	}
}

// grantsOf renders a Principal's Grants for the payload. Rendered rather than structured: a Grant is
// a Role name and an Anchor, its String is what a log line has always shown, and a record nobody
// queries by does not need the two apart until something does.
func grantsOf(p authz.Principal) []string {
	grants := p.Grants()
	if len(grants) == 0 {
		return nil
	}
	rendered := make([]string, 0, len(grants))
	for _, g := range grants {
		rendered = append(rendered, g.String())
	}
	return rendered
}

// equal reports whether two Records say the same thing. For the repository specification, which has to
// compare a Record it wrote against the one it read back and cannot use == on a struct holding slices.
// Unexported: no production code compares two Records, and nothing outside this package holds one.
func (r Record) equal(other Record) bool {
	return r.ID == other.ID &&
		r.OccurredAt.Equal(other.OccurredAt) &&
		r.Action == other.Action &&
		r.Outcome == other.Outcome &&
		r.Principal == other.Principal &&
		slices.Equal(r.Resource, other.Resource) &&
		r.Details.Attribution == other.Details.Attribution &&
		slices.Equal(r.Details.Grants, other.Details.Grants) &&
		r.Details.Reason == other.Details.Reason
}

// validate is what a Record arriving from the wire has to satisfy to be worth a row. Checked at the
// consumer rather than trusted: a payload that fails here is a fault in the message, and a fault in
// the message is abandoned rather than retried, so the check is what keeps a poisoned record from
// becoming a hot loop.
func (r Record) validate() error {
	if r.ID == "" {
		return errors.New("record has no id")
	}
	if r.OccurredAt.IsZero() {
		return errors.New("record has no instant")
	}
	if _, err := authz.ParseAction(string(r.Action)); err != nil {
		return err
	}
	if _, err := authz.ParseOutcome(string(r.Outcome)); err != nil {
		return err
	}
	if len(r.Resource) == 0 || slices.Contains(r.Resource, "") {
		return fmt.Errorf("record names no well-formed resource: %v", r.Resource)
	}
	// A Principal is absent under exactly one outcome. Stated as an invariant so that a record
	// naming nobody cannot arrive as an ordinary refusal, which is the distinction a security
	// reviewer reads this table for.
	if (r.Principal == "") != (r.Outcome == authz.NoPrincipal) {
		return fmt.Errorf("record has principal %q under outcome %q", r.Principal, r.Outcome)
	}
	return nil
}
