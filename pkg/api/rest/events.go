package rest

import (
	"net/http"
	"strconv"

	"github.com/kannon-email/kannon/internal/stats"
	"github.com/kannon-email/kannon/internal/tracking"
)

func registerEvents(mux *http.ServeMux, service *stats.Service) {
	mux.Handle("GET /v1beta1/domains/{domain}/stats/events", handler(func(w http.ResponseWriter, r *http.Request) error {
		d, err := parseDomainPath(r)
		if err != nil {
			return err
		}
		input, err := parseEventQuery(r.URL.RawQuery)
		if err != nil {
			return err
		}
		page, err := service.QueryEvents(r.Context(), d, input)
		if err != nil {
			return err
		}

		items := []map[string]any{}
		for _, s := range page.Items {
			items = append(items, eventWire(s))
		}
		var next *string
		if page.NextCursor != "" {
			next = &page.NextCursor
		}
		writeJSON(w, 200, map[string]any{"items": items, "next_cursor": next})
		return nil
	}))
}

func eventWire(s *stats.Stat) map[string]any {
	typ := string(s.Type)
	if s.Type == stats.TypeAccepted {
		typ = string(EventTypeValidated)
	}
	data := map[string]any{}
	o := s.Outcome
	v := map[string]any{"id": strconv.FormatInt(int64(s.ID), 10), domainParameter: s.Domain.String(), batchIDParameter: s.MessageID, "timestamp": s.Timestamp.UTC(), typeParameter: typ, "data": data}
	if s.Email != "" {
		v["email"] = s.Email
	}
	switch s.Type {
	case stats.TypeRejected, stats.TypeFailed:
		data["reason"] = o.Reason()
	case stats.TypeBounce:
		data["smtp_code"] = o.Code()
		data["detail"] = o.Msg()
		data["permanent"] = o.Permanent()
	case stats.TypeOpened, stats.TypeClicked:
		mode := s.EngagementTrackingMode()
		v["tracking_mode"] = mode
		if mode == tracking.ModePseudonymous {
			delete(v, "email")
			v["tracking_identity"] = s.Email
		}
		if mode == tracking.ModeFull {
			if o.IP() != "" {
				data["ip"] = o.IP()
			}
			if o.UserAgent() != "" {
				data["user_agent"] = o.UserAgent()
			}
		}
		if s.Type == stats.TypeClicked {
			data["url"] = o.URL()
		}
	}
	return v
}

func parseEventQuery(raw string) (stats.EventQuery, error) {
	q, err := parseQuery(raw, fromParameter, "to", batchIDParameter, limitParameter, cursorParameter, typeParameter)
	if err != nil {
		return stats.EventQuery{}, err
	}
	input := stats.EventQuery{Limit: 100, BatchID: q.Get(batchIDParameter), Cursor: q.Get(cursorParameter)}
	input.Range, err = timeRange(q)
	if err != nil {
		return input, err
	}
	if q.Has(limitParameter) {
		input.Limit, err = strconv.Atoi(q.Get(limitParameter))
	}
	if err != nil || (q.Has(batchIDParameter) && input.BatchID == "") {
		return input, problemError(400, "invalid_query_parameter")
	}
	if q.Has(cursorParameter) && input.Cursor == "" {
		return input, problemError(400, "invalid_cursor")
	}
	for _, t := range q[typeParameter] {
		switch t {
		case string(EventTypeValidated):
			t = "accepted"
		case "accepted":
			return input, problemError(400, "invalid_query_parameter")
		}
		input.Types = append(input.Types, stats.Type(t))
	}
	return input, nil
}
