package rest

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/url"
	"time"

	"github.com/kannon-email/kannon/internal/intake"
	openapi_types "github.com/oapi-codegen/runtime/types"
)

type historyCursor struct {
	Scope, ID string
	Time      time.Time
}

func batchSummary(b intake.BatchHistory) BatchSummary {
	c := b.Counts
	counts := DeliveryCounts{Pending: c[intake.DeliveryPending], Scheduled: c[intake.DeliveryScheduled], Sending: c[intake.DeliverySending], Delivered: c[intake.DeliveryDelivered], Rejected: c[intake.DeliveryRejected], Bounced: c[intake.DeliveryBounced], Failed: c[intake.DeliveryFailed]}
	from := From{Email: openapi_types.Email(b.Sender.Email)}
	if b.Sender.Alias != "" {
		from.Name = &b.Sender.Alias
	}
	return BatchSummary{Id: b.ID, Domain: b.Domain, From: from, Subject: b.Subject, CreatedAt: b.CreatedAt, ScheduledAt: b.ScheduledAt, CompletedAt: b.CompletedAt, ExpiresAt: b.ExpiresAt(), AcceptedCount: b.AcceptedCount, RejectedCount: b.RejectedCount, DeliveryCounts: counts, Status: BatchSummaryStatus(b.Status())}
}
func registerBatchHistory(mux *http.ServeMux, s *intake.InlineService) {
	mux.Handle("GET /v1beta1/domains/{domain}/batches/{batch_id}", handler(func(w http.ResponseWriter, r *http.Request) error {
		d, err := parseDomainPath(r)
		if err != nil {
			return err
		}
		if _, err := parseQuery(r.URL.RawQuery); err != nil {
			return err
		}
		id := r.PathValue(batchIDParameter)

		b, e := s.ReadBatch(r.Context(), d, id)
		if e != nil {
			return e
		}
		a := batchSummary(*b)
		out := Batch{Id: a.Id, Domain: a.Domain, From: a.From, Subject: a.Subject, CreatedAt: a.CreatedAt, ScheduledAt: a.ScheduledAt, CompletedAt: a.CompletedAt, ExpiresAt: a.ExpiresAt, AcceptedCount: a.AcceptedCount, RejectedCount: a.RejectedCount, DeliveryCounts: a.DeliveryCounts, Status: BatchStatus(a.Status), Tracking: TrackingPolicy{Opens: TrackingMode(b.Tracking.Opens), Links: TrackingMode(b.Tracking.Links)}, Attachments: []AttachmentMetadata{}}
		if h := b.Headers; len(h.To)+len(h.Cc) > 0 {
			out.Headers = &Headers{To: &h.To, Cc: &h.Cc}
		}
		if !b.OneClickUnsubscribe.IsZero() {
			out.OneClickUnsubscribe = &Unsubscribe{UrlTemplate: b.OneClickUnsubscribe.URLTemplate}
		}
		if b.SourceTemplateID != "" {
			out.Content.SourceTemplateId = &b.SourceTemplateID
		}
		out.Content.Html = b.HTML
		out.Content.Type = "html"
		for _, att := range b.Attachments {
			out.Attachments = append(out.Attachments, AttachmentMetadata{Filename: att.Filename, SizeBytes: att.SizeBytes})
		}
		writeJSON(w, 200, out)
		return nil
	}))
	mux.Handle("GET /v1beta1/domains/{domain}/batches", handler(func(w http.ResponseWriter, r *http.Request) error {
		d, err := parseDomainPath(r)
		if err != nil {
			return err
		}
		q, scope, err := parseBatchHistoryQuery(r.URL.RawQuery, d.String())
		if err != nil {
			return err
		}
		limit := q.Limit
		q.Limit++
		rows, e := s.ListBatches(r.Context(), d, q)
		if e != nil {
			return e
		}
		out := BatchSummaryPage{Items: []BatchSummary{}}
		if len(rows) > limit {
			rows = rows[:limit]
			out.NextCursor = encodeHistoryCursor(scope, rows[limit-1].ID, rows[limit-1].CreatedAt)
		}
		for _, b := range rows {
			out.Items = append(out.Items, batchSummary(b))
		}
		writeJSON(w, 200, out)
		return nil
	}))
	mux.Handle("GET /v1beta1/domains/{domain}/batches/{batch_id}/deliveries", handler(func(w http.ResponseWriter, r *http.Request) error {
		d, err := parseDomainPath(r)
		if err != nil {
			return err
		}
		id := r.PathValue(batchIDParameter)
		q, scope, err := parseDeliveryHistoryQuery(r.URL.RawQuery, d.String(), id)
		if err != nil {
			return err
		}
		limit := q.Limit
		q.Limit++

		rows, e := s.ListDeliveries(r.Context(), d, id, q)
		if e != nil {
			return e
		}
		out := DeliverySummaryPage{Items: []DeliverySummary{}}
		if len(rows) > limit {
			rows = rows[:limit]
			out.NextCursor = encodeHistoryCursor(scope, rows[limit-1].ID, time.Time{})
		}
		for _, a := range rows {
			out.Items = append(out.Items, DeliverySummary{Id: a.ID, BatchId: a.BatchID, Domain: a.Domain, Email: a.Email, Status: DeliveryStatus(a.Status), CreatedAt: a.CreatedAt, ScheduledAt: a.ScheduledAt, UpdatedAt: a.UpdatedAt})
		}
		writeJSON(w, 200, out)
		return nil
	}))
}

func parseBatchHistoryQuery(raw, domain string) (intake.HistoryQuery, string, error) {
	v, err := parseQuery(raw, limitParameter, cursorParameter)
	if err != nil {
		return intake.HistoryQuery{}, "", err
	}
	scope := domain + "::false:"
	q, err := historyPage(v, scope, true)
	return q, scope, err
}

func parseDeliveryHistoryQuery(raw, domain, batchID string) (intake.HistoryQuery, string, error) {
	v, err := parseQuery(raw, limitParameter, cursorParameter, statusKey)
	if err != nil {
		return intake.HistoryQuery{}, "", err
	}
	var status intake.DeliveryStatus
	if v.Has(statusKey) {
		var ok bool
		if status, ok = intake.ParseDeliveryStatus(v.Get(statusKey)); !ok {
			return intake.HistoryQuery{}, "", problemError(400, "invalid_query_parameter")
		}
	}
	scope := domain + ":" + batchID + ":true:" + string(status)
	q, err := historyPage(v, scope, false)
	q.Status = status
	return q, scope, err
}

func historyPage(v url.Values, scope string, requireTime bool) (intake.HistoryQuery, error) {
	limit, err := pageLimit(v)
	q := intake.HistoryQuery{Limit: limit}
	if err != nil {
		return q, err
	}
	if v.Has(cursorParameter) {
		raw, e := base64.RawURLEncoding.DecodeString(v.Get(cursorParameter))
		var c historyCursor
		if e != nil || json.Unmarshal(raw, &c) != nil || c.Scope != scope || c.ID == "" || (requireTime && c.Time.IsZero()) {
			return q, problemError(400, "invalid_cursor")
		}
		q.AfterID, q.AfterTime = c.ID, c.Time
	}
	return q, nil
}

func encodeHistoryCursor(scope, id string, t time.Time) *string {
	raw, err := json.Marshal(historyCursor{scope, id, t})
	if err != nil {
		return nil
	}
	v := base64.RawURLEncoding.EncodeToString(raw)
	return &v
}
