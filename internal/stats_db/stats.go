package sq

type StatsType string

const (
	StatsTypePrepared  StatsType = "accepted"
	StatsTypeDelivered StatsType = "delivered"
	StatsTypeOpened    StatsType = "opened"
	StatsTypeClicked   StatsType = "clicked"
	StatsTypeBounce    StatsType = "bounced"
)
