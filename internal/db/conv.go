package sqlc

import (
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func PgTimestamp(t *timestamppb.Timestamp) pgtype.Timestamp {
	return pgtype.Timestamp{
		Time:  t.AsTime(),
		Valid: true,
	}
}

func PgTimestampFromTime(t time.Time) pgtype.Timestamp {
	return pgtype.Timestamp{
		Time:  t,
		Valid: true,
	}
}
