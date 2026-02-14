package statsv2

import (
	"context"
	"errors"

	"connectrpc.com/connect"
	"github.com/kannon-email/kannon/internal/stats"
	"github.com/kannon-email/kannon/proto/kannon/stats/apiv2"
	statsv2connect "github.com/kannon-email/kannon/proto/kannon/stats/apiv2/apiv2connect"
	"github.com/kannon-email/kannon/proto/kannon/stats/types"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type statsAPIConnectAdapter struct {
	service *stats.Service
}

func (s *statsAPIConnectAdapter) GetAggregatedStats(ctx context.Context, req *connect.Request[apiv2.GetAggregatedStatsReq]) (*connect.Response[apiv2.GetAggregatedStatsRes], error) {
	if req.Msg.Domain == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("domain is required"))
	}
	if req.Msg.FromDate == nil || req.Msg.ToDate == nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("from_date and to_date are required"))
	}
	from := req.Msg.FromDate.AsTime()
	to := req.Msg.ToDate.AsTime()
	if !from.Before(to) {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("from_date must be before to_date"))
	}

	timeRange := stats.TimeRange{
		Start: from,
		Stop:  to,
	}

	results, err := s.service.QueryAggregatedStats(ctx, req.Msg.Domain, timeRange)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	pbStats := make([]*types.StatsAggregated, 0, len(results))
	for _, r := range results {
		pbStats = append(pbStats, &types.StatsAggregated{
			Type:      string(r.Type),
			Timestamp: timestamppb.New(r.Timestamp),
			Count:     r.Count,
		})
	}

	return connect.NewResponse(&apiv2.GetAggregatedStatsRes{
		Stats: pbStats,
	}), nil
}

func NewStatsAPIService(service *stats.Service) statsv2connect.StatsApiV2Handler {
	return &statsAPIConnectAdapter{service: service}
}
