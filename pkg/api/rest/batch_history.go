package rest

import (
	"net/http"

	"github.com/kannon-email/kannon/internal/intake"
	openapi_types "github.com/oapi-codegen/runtime/types"
)

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
		if r := b.ReplyTo; !r.IsZero() {
			out.ReplyTo = &ReplyTo{Email: openapi_types.Email(r.Email)}
			if r.Alias != "" {
				out.ReplyTo.Name = &r.Alias
			}
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
		var status intake.DeliveryStatus
		if q.Has(statusKey) {
			var ok bool
			if status, ok = intake.ParseDeliveryStatus(q.Get(statusKey)); !ok {
				return problemError(400, "invalid_query_parameter")
			}
		}
		page, err := readPage(q, func(id string) bool { return id != "" }, "deliveries:id:asc", d.String(), id, string(status))
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
