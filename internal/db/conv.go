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
