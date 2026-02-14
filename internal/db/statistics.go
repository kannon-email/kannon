package sqlc

// StatsType is the database representation of stats event types.
// It is used by sqlc-generated code and must remain in this package.
type StatsType string

const (
	StatsTypeAccepted  StatsType = "accepted"
	StatsTypeRejected  StatsType = "rejected"
	StatsTypeDelivered StatsType = "delivered"
	StatsTypeOpened    StatsType = "opened"
	StatsTypeClicked   StatsType = "clicked"
	StatsTypeBounce    StatsType = "bounced"
	StatsTypeError     StatsType = "error"
	StatsTypeUnknown   StatsType = "unknown"
)
