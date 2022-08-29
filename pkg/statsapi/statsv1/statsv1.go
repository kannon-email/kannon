package statsv1

import (
	"github.com/ludusrusso/kannon/generated/pb/stats/apiv1"
	sq "github.com/ludusrusso/kannon/internal/stats_db"
)

func NewStatsAPIService(q *sq.Queries) apiv1.StatsApiV1Server {
	return &api{
		q: q,
	}
}
