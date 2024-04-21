package statsv1

import (
	sq "github.com/ludusrusso/kannon/internal/db"
	api "github.com/ludusrusso/kannon/proto/kannon/stats/apiv1/apiv1connect"
)

func NewStatsAPIService(q *sq.Queries) api.StatsApiV1Handler {
	return &a{
		q: q,
	}
}
