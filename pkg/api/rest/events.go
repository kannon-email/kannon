package rest

import (
	"net/http"
	"net/url"
	"slices"
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
		q, input, err := parseEventQuery(r.URL.RawQuery)
		if err != nil {
			return err
		}
		types := slices.Clone(input.Types)
		slices.Sort(types)
		page, err := readPage(q, func(p stats.EventPosition) bool {
			return p.ID > 0 && !p.Timestamp.IsZero()
		}, "stats-events:timestamp,id:desc", d.String(), input.Range.Start.UTC(), input.Range.Stop.UTC(), slices.Compact(types), input.BatchID)
		if err != nil {
			return err
		}
		rows, err := service.QueryEvents(r.Context(), d, input, page.keyset())
		if err != nil {
			return err
		}
		rows, next, err := writePage(page, rows, stats.EventPositionOf)
		if err != nil {
			return err
		}

		items := []map[string]any{}
		for _, s := range rows {
			items = append(items, eventWire(s))
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

// parseEventQuery reads the filters; the returned values still carry limit and cursor for readPage.
func parseEventQuery(raw string) (url.Values, stats.EventQuery, error) {
	q, err := parseQuery(raw, fromParameter, "to", batchIDParameter, limitParameter, cursorParameter, typeParameter)
	if err != nil {
		return nil, stats.EventQuery{}, err
	}
	input := stats.EventQuery{BatchID: q.Get(batchIDParameter)}
	input.Range, err = timeRange(q)
	if err != nil {
		return nil, input, err
	}
	if q.Has(batchIDParameter) && input.BatchID == "" {
		return nil, input, problemError(400, "invalid_query_parameter")
	}
	for _, t := range q[typeParameter] {
		switch t {
		case string(EventTypeValidated):
			t = "accepted"
		case "accepted":
			return nil, input, problemError(400, "invalid_query_parameter")
		}
		input.Types = append(input.Types, stats.Type(t))
	}
	return q, input, nil
}
