package rest

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/kannon-email/kannon/internal/intake"
	openapi_types "github.com/oapi-codegen/runtime/types"
)

func batchSummary(b intake.BatchHistory) BatchSummary {
	counts := DeliveryCounts{Pending: b.Counts["pending"], Scheduled: b.Counts["scheduled"], Sending: b.Counts["sending"], Delivered: b.Counts["delivered"], Rejected: b.Counts["rejected"], Bounced: b.Counts["bounced"], Failed: b.Counts["failed"]}
	status := BatchSummaryStatus("pending")
	var expires *time.Time
	if b.CompletedAt != nil {
		status = "completed"
		t := b.CompletedAt.Add(30 * 24 * time.Hour)
		expires = &t
	} else if counts.Sending > 0 || counts.Delivered+counts.Rejected+counts.Bounced+counts.Failed > 0 {
		status = "processing"
	}
	from := From{Email: openapi_types.Email(b.Sender.Email)}
	if b.Sender.Alias != "" {
		from.Name = &b.Sender.Alias
	}
	return BatchSummary{Id: b.ID, Domain: b.Domain, From: from, Subject: b.Subject, CreatedAt: b.CreatedAt, ScheduledAt: b.ScheduledAt, CompletedAt: b.CompletedAt, ExpiresAt: expires, AcceptedCount: b.AcceptedCount, RejectedCount: b.RejectedCount, DeliveryCounts: counts, Status: status}
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
		var headers struct {
			To          []string `json:"to"`
			Cc          []string `json:"cc"`
			Unsubscribe *struct {
				URLTemplate string `json:"url_template"`
			} `json:"one_click_unsubscribe"`
		}
		if err := json.Unmarshal(b.Headers, &headers); err != nil {
			return err
		}
		a := batchSummary(*b)
		out := Batch{Id: a.Id, Domain: a.Domain, From: a.From, Subject: a.Subject, CreatedAt: a.CreatedAt, ScheduledAt: a.ScheduledAt, CompletedAt: a.CompletedAt, ExpiresAt: a.ExpiresAt, AcceptedCount: a.AcceptedCount, RejectedCount: a.RejectedCount, DeliveryCounts: a.DeliveryCounts, Status: BatchStatus(a.Status), Tracking: TrackingPolicy{Opens: TrackingMode(b.Tracking.Opens), Links: TrackingMode(b.Tracking.Links)}, Attachments: []AttachmentMetadata{}}
		if len(headers.To)+len(headers.Cc) > 0 {
			out.Headers = &Headers{To: &headers.To, Cc: &headers.Cc}
		}
		if headers.Unsubscribe != nil {
			out.OneClickUnsubscribe = &Unsubscribe{UrlTemplate: headers.Unsubscribe.URLTemplate}
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
		q, err := parseQuery(r.URL.RawQuery, limitParameter, cursorParameter)
		if err != nil {
			return err
		}
		page, err := readPage(q, func(p intake.BatchPosition) bool {
			return p.ID != "" && !p.CreatedAt.IsZero()
		}, "batches:created_at,id:desc", d.String())
		if err != nil {
			return err
		}
		rows, err := s.ListBatches(r.Context(), d, page.keyset())
		if err != nil {
			return err
		}
		rows, next, err := writePage(page, rows, intake.BatchHistory.PositionOf)
		if err != nil {
			return err
		}
		out := BatchSummaryPage{Items: []BatchSummary{}, NextCursor: next}
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
		q, err := parseQuery(r.URL.RawQuery, limitParameter, cursorParameter, statusKey)
		if err != nil {
			return err
		}
		status := q.Get(statusKey)
		if q.Has(statusKey) {
			switch status {
			case "pending", "scheduled", "sending", "delivered", "rejected", "bounced", "failed":
			default:
				return problemError(400, "invalid_query_parameter")
			}
		}
		page, err := readPage(q, func(id string) bool { return id != "" }, "deliveries:id:asc", d.String(), id, status)
		if err != nil {
			return err
		}
		rows, err := s.ListDeliveries(r.Context(), d, id, status, page.keyset())
		if err != nil {
			return err
		}
		rows, next, err := writePage(page, rows, func(a intake.DeliveryHistory) string { return a.ID })
		if err != nil {
			return err
		}
		out := DeliverySummaryPage{Items: []DeliverySummary{}, NextCursor: next}
		for _, a := range rows {
			out.Items = append(out.Items, DeliverySummary{Id: a.ID, BatchId: a.BatchID, Domain: a.Domain, Email: a.Email, Status: DeliveryStatus(a.Status), CreatedAt: a.CreatedAt, ScheduledAt: a.ScheduledAt, UpdatedAt: a.UpdatedAt})
		}
		writeJSON(w, 200, out)
		return nil
	}))
}
