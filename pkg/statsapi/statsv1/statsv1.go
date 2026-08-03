package statsv1

import (
	"context"

	"connectrpc.com/connect"
	"github.com/kannon-email/kannon/internal/authzconnect"
	"github.com/kannon-email/kannon/internal/stats"
	"github.com/kannon-email/kannon/proto/kannon/stats/apiv1"
	statsv1connect "github.com/kannon-email/kannon/proto/kannon/stats/apiv1/apiv1connect"
)

// Adapter to Connect handler interface
//
// Both procedures read a Domain's per-Delivery statistics, which the stats service
// guards: the rows carry Recipient addresses and, under Full tracking, IP addresses
// and user agents. A refusal therefore has to arrive as CodePermissionDenied rather
// than as the CodeInternal this adapter used to answer for everything — see
// authzconnect.Error.

type statsAPIConnectAdapter struct {
	impl *statsV1Impl
}

func (s *statsAPIConnectAdapter) GetStats(ctx context.Context, req *connect.Request[apiv1.GetStatsReq]) (*connect.Response[apiv1.GetStatsRes], error) {
	resp, err := s.impl.GetStats(ctx, req.Msg)
	if err != nil {
		return nil, authzconnect.Error(err, connect.CodeInternal)
	}
	return connect.NewResponse(resp), nil
}

func (s *statsAPIConnectAdapter) GetStatsAggregated(ctx context.Context, req *connect.Request[apiv1.GetStatsAggregatedReq]) (*connect.Response[apiv1.GetStatsAggregatedRes], error) {
	resp, err := s.impl.GetStatsAggregated(ctx, req.Msg)
	if err != nil {
		return nil, authzconnect.Error(err, connect.CodeInternal)
	}
	return connect.NewResponse(resp), nil
}

func NewStatsAPIService(service *stats.Service) statsv1connect.StatsApiV1Handler {
	return &statsAPIConnectAdapter{
		impl: &statsV1Impl{service: service},
	}
}
