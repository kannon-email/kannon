package statsv1

import (
	sq "github.com/ludusrusso/kannon/internal/db"
	"github.com/ludusrusso/kannon/proto/kannon/stats/apiv1"
)

func NewStatsAPIService(q *sq.Queries) apiv1.StatsApiV1Server {
	return &a{
		q: q,
	}
}
