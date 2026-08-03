package sqlc

import (
	"time"

	"github.com/jackc/pgx/v5/pgtype"
)

func PgTimestampFromTime(t time.Time) pgtype.Timestamp {
	return pgtype.Timestamp{
		Time:  t,
		Valid: true,
	}
}

// PgIntervalFromDuration converts a Go duration into a Postgres interval, so a
// threshold expressed in Go can be applied against NOW() inside the database
// rather than against a timestamp computed in the process's own clock.
func PgIntervalFromDuration(d time.Duration) pgtype.Interval {
	return pgtype.Interval{
		Microseconds: d.Microseconds(),
		Valid:        true,
	}
}
