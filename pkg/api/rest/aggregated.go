package rest

import (
	"net/http"
	"time"

	"github.com/kannon-email/kannon/internal/stats"
)

func registerAggregated(mux *http.ServeMux, service *stats.Service) {
	mux.Handle("GET /v1beta1/domains/{domain}/stats/aggregated", handler(func(w http.ResponseWriter, r *http.Request) error {
		d, err := parseDomainPath(r)
		if err != nil {
			return err
		}
		input, err := parseAggregateQuery(r.URL.RawQuery)
		if err != nil {
			return err
		}
		buckets, err := service.QueryAggregateSeries(r.Context(), d, input.Range, input.Group)
		if err != nil {
			return err
		}

		items := make([]map[string]any, 0, len(buckets))
		for _, b := range buckets {
			counts := map[string]int64{string(EventTypeValidated): b.Counts[stats.TypeAccepted], "rejected": b.Counts[stats.TypeRejected], "delivered": b.Counts[stats.TypeDelivered], "bounced": b.Counts[stats.TypeBounce], "failed": b.Counts[stats.TypeFailed], "opened": b.Counts[stats.TypeOpened], "clicked": b.Counts[stats.TypeClicked]}
			items = append(items, map[string]any{"start": b.Start, "end": b.End, "counts": counts})
		}
		writeJSON(w, 200, map[string]any{fromParameter: input.Range.Start.UTC(), "to": input.Range.Stop.UTC(), groupByParameter: input.GroupBy, "buckets": items})
		return nil
	}))
}

type aggregateQuery struct {
	Range   stats.TimeRange
	Group   time.Duration
	GroupBy string
}

func parseAggregateQuery(raw string) (aggregateQuery, error) {
	q, err := parseQuery(raw, fromParameter, "to", groupByParameter)
	if err != nil {
		return aggregateQuery{}, err
	}
	span, err := timeRange(q)
	if err != nil {
		return aggregateQuery{}, err
	}
	group, err := time.ParseDuration(q.Get(groupByParameter))
	if err != nil {
		return aggregateQuery{}, problemError(400, "invalid_query_parameter")
	}
	return aggregateQuery{Range: span, Group: group, GroupBy: q.Get(groupByParameter)}, nil
}
