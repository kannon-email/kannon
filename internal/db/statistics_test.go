package sqlc

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	types "github.com/ludusrusso/kannon/proto/kannon/stats/types"
	"github.com/stretchr/testify/require"
)

func TestReadAndWriteStats(t *testing.T) {
	err := q.InsertStat(context.Background(), InsertStatParams{
		Email:     "test@test.com",
		MessageID: "123",
		Type:      StatsTypeAccepted,
		Timestamp: pgtype.Timestamp{Time: time.Now(), Valid: true},
		Domain:    "test.com",
		Data: &types.StatsData{
			Data: &types.StatsData_Accepted{
				Accepted: &types.StatsDataAccepted{},
			},
		},
	})

	require.Nil(t, err)

	stats, err := q.QueryStats(context.Background(), QueryStatsParams{
		Domain: "test.com",
		Start:  pgtype.Timestamp{Time: time.Now().Add(-1 * time.Hour), Valid: true},
		Stop:   pgtype.Timestamp{Time: time.Now().Add(1 * time.Hour), Valid: true},
		Skip:   0,
		Take:   10,
	})

	require.Nil(t, err)
	require.Equal(t, 1, len(stats))
}
