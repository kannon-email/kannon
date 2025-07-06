package statsv1

import (
	"context"

	"github.com/jackc/pgx/v5/pgtype"
	sq "github.com/kannon-email/kannon/internal/db"
	"github.com/kannon-email/kannon/proto/kannon/stats/apiv1"
	"github.com/kannon-email/kannon/proto/kannon/stats/types"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type a struct {
	q *sq.Queries
}

func toPgTimestamp(t *timestamppb.Timestamp) pgtype.Timestamp {
	return pgtype.Timestamp{
		Time:  t.AsTime(),
		Valid: true,
	}
}

func (a *a) GetStats(ctx context.Context, req *apiv1.GetStatsReq) (*apiv1.GetStatsRes, error) {
	stats, err := a.q.QueryStats(ctx, sq.QueryStatsParams{
		Domain: req.Domain,
		Start:  toPgTimestamp(req.FromDate),
		Stop:   toPgTimestamp(req.ToDate),
		Skip:   int32(req.Skip),
		Take:   int32(req.Take),
	})
	if err != nil {
		return nil, err
	}

	total, err := a.q.CountQueryStats(ctx, sq.CountQueryStatsParams{
		Domain: req.Domain,
		Start:  toPgTimestamp(req.FromDate),
		Stop:   toPgTimestamp(req.ToDate),
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
		Start:  toPgTimestamp(req.FromDate),
		Stop:   toPgTimestamp(req.ToDate),
	})
	if err != nil {
		return nil, err
	}

	pbStats := make([]*types.StatsAggregated, 0, len(stats))
	for _, s := range stats {
		pbStats = append(pbStats, &types.StatsAggregated{
			Type:      string(s.Type),
			Timestamp: timestamppb.New((s.Ts.Time)),
			Count:     uint32(s.Count),
		})
	}

	return &apiv1.GetStatsAggregatedRes{
		Stats: pbStats,
	}, nil
}
