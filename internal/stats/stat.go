package stats

import (
	"errors"
	"time"

	"github.com/kannon-email/kannon/proto/kannon/stats/types"
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
	TypeUnknown   Type = "unknown"
)

var ErrStatNotFound = errors.New("stat not found")

// Stat is the domain entity for a statistics event.
type Stat struct {
	ID        int32
	Type      Type
	Email     string
	MessageID string
	Domain    string
	Timestamp time.Time
	Data      *types.StatsData
}

// NewStat creates a new Stat from an incoming stats event.
func NewStat(email, messageID, domain string, timestamp time.Time, data *types.StatsData) *Stat {
	return &Stat{
		Type:      DetermineType(data),
		Email:     email,
		MessageID: messageID,
		Domain:    domain,
		Timestamp: timestamp,
		Data:      data,
	}
}

// LoadStat reconstructs a Stat from persistence.
func LoadStat(id int32, stype Type, email, messageID, domain string, timestamp time.Time, data *types.StatsData) *Stat {
	return &Stat{
		ID:        id,
		Type:      stype,
		Email:     email,
		MessageID: messageID,
		Domain:    domain,
		Timestamp: timestamp,
		Data:      data,
	}
}

// DetermineType inspects the protobuf data to determine the stats type.
func DetermineType(d *types.StatsData) Type {
	if d == nil {
		return TypeUnknown
	}
	switch d.Data.(type) {
	case *types.StatsData_Accepted:
		return TypeAccepted
	case *types.StatsData_Rejected:
		return TypeRejected
	case *types.StatsData_Bounced:
		return TypeBounce
	case *types.StatsData_Clicked:
		return TypeClicked
	case *types.StatsData_Delivered:
		return TypeDelivered
	case *types.StatsData_Opened:
		return TypeOpened
	case *types.StatsData_Error:
		return TypeError
	default:
		return TypeUnknown
	}
}

// DetermineTypeFromStats inspects a full Stats protobuf message to determine the type.
func DetermineTypeFromStats(s *types.Stats) Type {
	return DetermineType(s.Data)
}

// DisplayName maps Type to a human-readable display string.
var DisplayName = map[Type]string{
	TypeAccepted:  "Accepted",
	TypeRejected:  "Rejected",
	TypeBounce:    "Bounced",
	TypeClicked:   "Clicked",
	TypeDelivered: "Delivered",
	TypeError:     "Send Error",
	TypeOpened:    "Opened",
	TypeUnknown:   "Unknown",
}
