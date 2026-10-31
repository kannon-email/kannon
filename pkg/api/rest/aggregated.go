package rest

import (
	"github.com/kannon-email/kannon/internal/stats"
	"github.com/kannon-email/kannon/internal/values"
	"net/http"
	"net/url"
	"time"
)

func registerAggregated(mux *http.ServeMux, service *stats.Service) {
	mux.Handle("GET /v1beta1/domains/{domain}/stats/aggregated", handler(func(w http.ResponseWriter, r *http.Request) error {
		d, err := values.Parse(r.PathValue(domainParameter))
		if err != nil {
			return problemError(400, "malformed_request")
		}
		q, err := url.ParseQuery(r.URL.RawQuery)
		if err != nil {
			return problemError(400, "invalid_query_parameter")
		}
		for k, v := range q {
			if k != fromParameter && k != "to" && k != groupByParameter {
				return problemError(400, "unknown_query_parameter")
			}
			if len(v) != 1 {
				return problemError(400, "invalid_query_parameter")
			}
		}
		from, e1 := time.Parse(time.RFC3339Nano, q.Get(fromParameter))
		to, e2 := time.Parse(time.RFC3339Nano, q.Get("to"))
		group, e3 := time.ParseDuration(q.Get(groupByParameter))
		if e1 != nil || e2 != nil || e3 != nil {
			return problemError(400, "invalid_query_parameter")
		}
		buckets, err := service.QueryAggregateSeries(r.Context(), d, stats.TimeRange{Start: from, Stop: to}, group)
		if err != nil {
			return err
		}

		items := make([]map[string]any, 0, len(buckets))
		for _, b := range buckets {
			counts := map[string]int64{string(EventTypeValidated): b.Counts[stats.TypeAccepted], "rejected": b.Counts[stats.TypeRejected], "delivered": b.Counts[stats.TypeDelivered], "bounced": b.Counts[stats.TypeBounce], "failed": b.Counts[stats.TypeFailed], "opened": b.Counts[stats.TypeOpened], "clicked": b.Counts[stats.TypeClicked]}
			items = append(items, map[string]any{"start": b.Start, "end": b.End, "counts": counts})
		}
		writeJSON(w, 200, map[string]any{fromParameter: from.UTC(), "to": to.UTC(), groupByParameter: q.Get(groupByParameter), "buckets": items})
		return nil
	}))
}
