package statsv1

import (
	"context"

	"github.com/kannon-email/kannon/internal/stats"
	"github.com/kannon-email/kannon/proto/kannon/stats/apiv1"
	"github.com/kannon-email/kannon/proto/kannon/stats/types"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type statsV1Impl struct {
	service *stats.Service
}

func (a *statsV1Impl) GetStats(ctx context.Context, req *apiv1.GetStatsReq) (*apiv1.GetStatsRes, error) {
	timeRange := stats.TimeRange{
		Start: req.FromDate.AsTime(),
		Stop:  req.ToDate.AsTime(),
	}
	page := stats.Pagination{
		Limit:  int(req.Take),
		Offset: int(req.Skip),
	}

	results, total, err := a.service.QueryStats(ctx, req.Domain, timeRange, page)
	if err != nil {
		return nil, err
	}

	pbStats := make([]*types.Stats, 0, len(results))
	for _, s := range results {
		pbStats = append(pbStats, statToPb(s))
	}

	return &apiv1.GetStatsRes{
		Total: total,
		Stats: pbStats,
	}, nil
}

func (a *statsV1Impl) GetStatsAggregated(ctx context.Context, req *apiv1.GetStatsAggregatedReq) (*apiv1.GetStatsAggregatedRes, error) {
	timeRange := stats.TimeRange{
		Start: req.FromDate.AsTime(),
		Stop:  req.ToDate.AsTime(),
	}

	results, err := a.service.QueryTimeline(ctx, req.Domain, timeRange)
	if err != nil {
		return nil, err
	}

	pbStats := make([]*types.StatsAggregated, 0, len(results))
	for _, s := range results {
		pbStats = append(pbStats, &types.StatsAggregated{
			Type:      string(s.Type),
			Timestamp: timestamppb.New(s.Timestamp),
			Count:     s.Count,
		})
	}

	return &apiv1.GetStatsAggregatedRes{
		Stats: pbStats,
	}, nil
}

func statToPb(s *stats.Stat) *types.Stats {
	return &types.Stats{
		MessageId: s.MessageID,
		Domain:    s.Domain,
		Email:     s.Email,
		Timestamp: timestamppb.New(s.Timestamp),
		Type:      string(s.Type),
		Data:      s.Data,
	}
}
