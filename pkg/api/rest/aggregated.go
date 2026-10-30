package rest

import (
	"errors"
	"github.com/kannon-email/kannon/internal/stats"
	"github.com/kannon-email/kannon/internal/values"
	"net/http"
	"net/url"
	"time"
)

func registerAggregated(mux *http.ServeMux, service *stats.Service) {
	mux.HandleFunc("GET /v1beta1/domains/{domain}/stats/aggregated", func(w http.ResponseWriter, r *http.Request) {
		d, err := values.Parse(r.PathValue("domain"))
		if err != nil {
			WriteProblem(w, r, 400, "malformed_request")
			return
		}
		q, err := url.ParseQuery(r.URL.RawQuery)
		if err != nil {
			WriteProblem(w, r, 400, "invalid_query_parameter")
			return
		}
		for k, v := range q {
			if k != "from" && k != "to" && k != "group_by" {
				WriteProblem(w, r, 400, "unknown_query_parameter")
				return
			}
			if len(v) != 1 {
				WriteProblem(w, r, 400, "invalid_query_parameter")
				return
			}
		}
		from, e1 := time.Parse(time.RFC3339Nano, q.Get("from"))
		to, e2 := time.Parse(time.RFC3339Nano, q.Get("to"))
		group, e3 := time.ParseDuration(q.Get("group_by"))
		if e1 != nil || e2 != nil || e3 != nil {
			WriteProblem(w, r, 400, "invalid_query_parameter")
			return
		}
		buckets, err := service.QueryAggregateSeries(r.Context(), d, stats.TimeRange{Start: from, Stop: to}, group)
		if err != nil {
			if errors.Is(err, stats.ErrAggregateQuery) {
				WriteProblem(w, r, 400, "invalid_query_parameter")
			} else {
				writeError(w, r, err)
			}
			return
		}
		items := make([]map[string]any, 0, len(buckets))
		for _, b := range buckets {
			counts := map[string]int64{"validated": b.Counts[stats.TypeAccepted], "rejected": b.Counts[stats.TypeRejected], "delivered": b.Counts[stats.TypeDelivered], "bounced": b.Counts[stats.TypeBounce], "failed": b.Counts[stats.TypeFailed], "opened": b.Counts[stats.TypeOpened], "clicked": b.Counts[stats.TypeClicked]}
			items = append(items, map[string]any{"start": b.Start, "end": b.End, "counts": counts})
		}
		writeJSON(w, 200, map[string]any{"from": from.UTC(), "to": to.UTC(), "group_by": q.Get("group_by"), "buckets": items})
	})
}
