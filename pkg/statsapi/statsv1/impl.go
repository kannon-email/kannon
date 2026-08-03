package statsv1

import (
	"context"

	"github.com/kannon-email/kannon/internal/stats"
	"github.com/kannon-email/kannon/internal/values"
	"github.com/kannon-email/kannon/proto/kannon/stats/apiv1"
	"github.com/kannon-email/kannon/proto/kannon/stats/types"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type statsV1Impl struct {
	service *stats.Service
}

// The requested domain arrives as a bare string, so it is canonicalised here rather than
// passed on: a query on a spelling that was never canonicalised would return no rows, which a
// caller cannot tell from "this Domain sent nothing".
func (a *statsV1Impl) GetStats(ctx context.Context, req *apiv1.GetStatsReq) (*apiv1.GetStatsRes, error) {
	domain, err := values.Parse(req.Domain)
	if err != nil {
		return nil, err
	}

	timeRange := stats.TimeRange{
		Start: req.FromDate.AsTime(),
		Stop:  req.ToDate.AsTime(),
	}
	page := stats.Pagination{
		Limit:  int(req.Take),
		Offset: int(req.Skip),
	}

	results, total, err := a.service.QueryStats(ctx, domain, timeRange, page)
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
	domain, err := values.Parse(req.Domain)
	if err != nil {
		return nil, err
	}

	timeRange := stats.TimeRange{
		Start: req.FromDate.AsTime(),
		Stop:  req.ToDate.AsTime(),
	}

	results, err := a.service.QueryTimeline(ctx, domain, timeRange)
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
		Domain:    s.Domain.String(),
		Email:     s.Email,
		Timestamp: timestamppb.New(s.Timestamp),
		Type:      string(s.Type),
		Data:      s.Data,
	}
}
