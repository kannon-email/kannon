package statsv1

import (
	"context"

	"connectrpc.com/connect"
	sq "github.com/ludusrusso/kannon/internal/db"
	"github.com/ludusrusso/kannon/proto/kannon/stats/apiv1"
	statsv1connect "github.com/ludusrusso/kannon/proto/kannon/stats/apiv1/apiv1connect"
)

// Adapter to Connect handler interface

type statsAPIConnectAdapter struct {
	impl *a
}

func (s *statsAPIConnectAdapter) GetStats(ctx context.Context, req *connect.Request[apiv1.GetStatsReq]) (*connect.Response[apiv1.GetStatsRes], error) {
	resp, err := s.impl.GetStats(ctx, req.Msg)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	return connect.NewResponse(resp), nil
}

func (s *statsAPIConnectAdapter) GetStatsAggregated(ctx context.Context, req *connect.Request[apiv1.GetStatsAggregatedReq]) (*connect.Response[apiv1.GetStatsAggregatedRes], error) {
	resp, err := s.impl.GetStatsAggregated(ctx, req.Msg)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	return connect.NewResponse(resp), nil
}

func NewStatsAPIService(q *sq.Queries) statsv1connect.StatsApiV1Handler {
	return &statsAPIConnectAdapter{
		impl: &a{q: q},
	}
}
