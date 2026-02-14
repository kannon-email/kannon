package stats

import "time"

// AggregatedStat represents a time-bucketed count of stats events.
type AggregatedStat struct {
	Type      Type
	Timestamp time.Time
	Count     int64
}
