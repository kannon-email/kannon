package statsv1

import (
	"context"

	sq "github.com/ludusrusso/kannon/internal/db"
	"github.com/ludusrusso/kannon/proto/kannon/stats/apiv1"
	"github.com/ludusrusso/kannon/proto/kannon/stats/types"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type a struct {
	q *sq.Queries
}

func (a *a) GetStats(ctx context.Context, req *apiv1.GetStatsReq) (*apiv1.GetStatsRes, error) {
	stats, err := a.q.QueryStats(ctx, sq.QueryStatsParams{
		Domain: req.Domain,
		Start:  req.FromDate.AsTime(),
		Stop:   req.ToDate.AsTime(),
		Skip:   int32(req.Skip),
		Take:   int32(req.Take),
	})
	if err != nil {
		return nil, err
	}

	total, err := a.q.CountQueryStats(ctx, sq.CountQueryStatsParams{
		Domain: req.Domain,
		Start:  req.FromDate.AsTime(),
		Stop:   req.ToDate.AsTime(),
	})
	if err != nil {
		return nil, err
	}

	pbStats := make([]*types.Stats, 0, len(stats))
	for _, s := range stats {
		pbStats = append(pbStats, s.Pb())
	}

	return &apiv1.GetStatsRes{
		Total: uint32(total),
		Stats: pbStats,
	}, nil
}

func (a *a) GetStatsAggregated(ctx context.Context, req *apiv1.GetStatsAggregatedReq) (*apiv1.GetStatsAggregatedRes, error) {
	stats, err := a.q.QueryStatsTimeline(ctx, sq.QueryStatsTimelineParams{
		Domain: req.Domain,
		Start:  req.FromDate.AsTime(),
		Stop:   req.ToDate.AsTime(),
	})
	if err != nil {
		return nil, err
	}

	pbStats := make([]*types.StatsAggregated, 0, len(stats))
	for _, s := range stats {
		pbStats = append(pbStats, &types.StatsAggregated{
			Type:      string(s.Type),
			Timestamp: timestamppb.New((s.Ts)),
			Count:     uint32(s.Count),
		})
	}

	return &apiv1.GetStatsAggregatedRes{
		Stats: pbStats,
	}, nil
}
