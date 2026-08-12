package stats

import (
	"time"

	"github.com/kannon-email/kannon/internal/values"
)

// Type represents the kind of statistics event.
type Type string

const (
	TypeAccepted  Type = "accepted"
	TypeRejected  Type = "rejected"
	TypeDelivered Type = "delivered"
	TypeOpened    Type = "opened"
	TypeClicked   Type = "clicked"
	TypeBounce    Type = "bounced"
	TypeError     Type = "error"
	TypeFailed    Type = "failed"
	TypeUnknown   Type = "unknown"
)

// Stat is the domain entity for a statistics event. Domain is the SenderDomain it belongs to, and is
// the same domain name a Domain is created under: a stat filed against a different spelling would be
// invisible to every query the tenant can make.
//
// Type and Outcome say overlapping things, and both are kept because the table
// stores them in separate columns: stats.type is written from the Outcome at
// insert time and read straight back out on the way to the API, so a row whose
// two columns disagree — one written by an older build, say — keeps reporting
// whatever its type column says rather than being silently reinterpreted.
type Stat struct {
	ID        int32
	Type      Type
	Email     string
	MessageID string
	Domain    values.DomainName
	Timestamp time.Time
	Outcome   Outcome
}

// NewStat creates a new Stat from an incoming stats event, typing the row off
// the Outcome it carries.
func NewStat(email, messageID string, domain values.DomainName, timestamp time.Time, outcome Outcome) *Stat {
	return &Stat{
		Type:      outcome.Type(),
		Email:     email,
		MessageID: messageID,
		Domain:    domain,
		Timestamp: timestamp,
		Outcome:   outcome,
	}
}

// LoadStat reconstructs a Stat from persistence. The type is taken from the
// caller rather than derived from the Outcome, because the two are separate
// columns and this is a read: the row is reported as it was stored.
func LoadStat(id int32, stype Type, email, messageID string, domain values.DomainName, timestamp time.Time, outcome Outcome) *Stat {
	return &Stat{
		ID:        id,
		Type:      stype,
		Email:     email,
		MessageID: messageID,
		Domain:    domain,
		Timestamp: timestamp,
		Outcome:   outcome,
	}
}

// DisplayName maps Type to a human-readable display string.
var DisplayName = map[Type]string{
	TypeAccepted:  "Accepted",
	TypeRejected:  "Rejected",
	TypeBounce:    "Bounced",
	TypeClicked:   "Clicked",
	TypeDelivered: "Delivered",
	TypeError:     "Send Error",
	TypeFailed:    "Failed",
	TypeOpened:    "Opened",
	TypeUnknown:   "Unknown",
}
