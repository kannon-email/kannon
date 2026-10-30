package rest

import (
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/kannon-email/kannon/internal/stats"
	"github.com/kannon-email/kannon/internal/tracking"
	"github.com/kannon-email/kannon/internal/values"
)

func registerEvents(mux *http.ServeMux, service *stats.Service) {
	mux.HandleFunc("GET /v1beta1/domains/{domain}/stats/events", func(w http.ResponseWriter, r *http.Request) {
		d, err := values.Parse(r.PathValue(domainParameter))
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
			switch k {
			case fromParameter, "to", batchIDParameter, limitParameter, cursorParameter, typeParameter:
			default:
				WriteProblem(w, r, 400, "unknown_query_parameter")
				return
			}
			if k != typeParameter && len(v) != 1 {
				WriteProblem(w, r, 400, "invalid_query_parameter")
				return
			}
		}
		input := stats.EventQuery{Limit: 100, BatchID: q.Get(batchIDParameter), Cursor: q.Get(cursorParameter)}
		input.Range.Start, err = time.Parse(time.RFC3339Nano, q.Get(fromParameter))
		if err == nil {
			input.Range.Stop, err = time.Parse(time.RFC3339Nano, q.Get("to"))
		}
		if err != nil {
			WriteProblem(w, r, 400, "invalid_query_parameter")
			return
		}
		if q.Has(limitParameter) {
			input.Limit, err = strconv.Atoi(q.Get(limitParameter))
		}
		if err != nil || (q.Has(batchIDParameter) && input.BatchID == "") {
			WriteProblem(w, r, 400, "invalid_query_parameter")
			return
		}
		if q.Has(cursorParameter) && input.Cursor == "" {
			WriteProblem(w, r, 400, "invalid_cursor")
			return
		}
		for _, t := range q[typeParameter] {
			switch t {
			case string(EventTypeValidated):
				t = "accepted"
			case "accepted":
				WriteProblem(w, r, 400, "invalid_query_parameter")
				return
			}
			input.Types = append(input.Types, stats.Type(t))
		}
		page, err := service.QueryEvents(r.Context(), d, input)
		if err != nil {
			switch {
			case errors.Is(err, stats.ErrEventCursor):
				WriteProblem(w, r, 400, "invalid_cursor")
			case errors.Is(err, stats.ErrEventQuery):
				WriteProblem(w, r, 400, "invalid_query_parameter")
			default:
				writeError(w, r, err)
			}
			return
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
	})
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
		mode := s.TrackingMode
		// Legacy rows predate retained modes. The reserved identity namespace is authoritative;
		// request detail proves Full; otherwise use the least revealing identified shape.
		if tracking.IsPseudonym(s.Email, s.Domain.String()) {
			mode = tracking.ModePseudonymous
		}
		if mode == "" {
			mode = tracking.ModeIdentified
			if o.IP() != "" || o.UserAgent() != "" {
				mode = tracking.ModeFull
			}
		}
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
