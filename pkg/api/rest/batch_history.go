package rest

import (
	"encoding/base64"
	"encoding/json"
	"github.com/kannon-email/kannon/internal/intake"
	"github.com/kannon-email/kannon/internal/values"
	openapi_types "github.com/oapi-codegen/runtime/types"
	"net/http"
	"strconv"
	"time"
)

type historyCursor struct {
	Scope, ID string
	Time      time.Time
}

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
	for _, path := range []string{"/v1beta1/domains/{domain}/batches", "/v1beta1/domains/{domain}/batches/{batch_id}", "/v1beta1/domains/{domain}/batches/{batch_id}/deliveries"} {
		mux.Handle("GET "+path, handler(func(w http.ResponseWriter, r *http.Request) error {
			d, err := values.Parse(r.PathValue(domainParameter))
			if err != nil {
				return problemError(400, "malformed_request")
			}
			id := r.PathValue(batchIDParameter)
			deliveries := r.Pattern == "GET /v1beta1/domains/{domain}/batches/{batch_id}/deliveries"
			detail := id != "" && !deliveries
			q := intake.HistoryQuery{Limit: 100}
			v := r.URL.Query()
			for k, vs := range v {
				if len(vs) != 1 || detail || (k != limitParameter && k != cursorParameter && (!deliveries || k != "status")) {
					return problemError(400, "invalid_query_parameter")
				}
			}
			if v.Has(limitParameter) {
				q.Limit, err = strconv.Atoi(v.Get(limitParameter))
				if err != nil || q.Limit < 1 || q.Limit > 1000 {
					return problemError(400, "invalid_query_parameter")
				}
			}
			q.Status = v.Get("status")
			if v.Has("status") {
				switch q.Status {
				case "pending", "scheduled", "sending", "delivered", "rejected", "bounced", "failed":
				default:
					return problemError(400, "invalid_query_parameter")
				}
			}
			scope := d.String() + ":" + id + ":" + strconv.FormatBool(deliveries) + ":" + q.Status
			if v.Has(cursorParameter) {
				raw, e := base64.RawURLEncoding.DecodeString(v.Get(cursorParameter))
				var c historyCursor
				if e != nil || json.Unmarshal(raw, &c) != nil || c.Scope != scope || c.ID == "" || (!deliveries && c.Time.IsZero()) {
					return problemError(400, "invalid_cursor")
				}
				q.AfterID = c.ID
				q.AfterTime = c.Time
			}

			if detail {
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
			}
			limit := q.Limit
			q.Limit++
			cursor := func(id string, t time.Time) *string {
				raw, err := json.Marshal(historyCursor{scope, id, t})
				if err != nil {
					return nil
				}
				v := base64.RawURLEncoding.EncodeToString(raw)
				return &v
			}
			if deliveries {
				rows, e := s.ListDeliveries(r.Context(), d, id, q)
				if e != nil {
					return e
				}
				out := DeliverySummaryPage{Items: []DeliverySummary{}}
				if len(rows) > limit {
					rows = rows[:limit]
					out.NextCursor = cursor(rows[limit-1].ID, time.Time{})
				}
				for _, a := range rows {
					out.Items = append(out.Items, DeliverySummary{Id: a.ID, BatchId: a.BatchID, Domain: a.Domain, Email: a.Email, Status: DeliveryStatus(a.Status), CreatedAt: a.CreatedAt, ScheduledAt: a.ScheduledAt, UpdatedAt: a.UpdatedAt})
				}
				writeJSON(w, 200, out)
				return nil
			}
			rows, e := s.ListBatches(r.Context(), d, q)
			if e != nil {
				return e
			}
			out := BatchSummaryPage{Items: []BatchSummary{}}
			if len(rows) > limit {
				rows = rows[:limit]
				out.NextCursor = cursor(rows[limit-1].ID, rows[limit-1].CreatedAt)
			}
			for _, b := range rows {
				out.Items = append(out.Items, batchSummary(b))
			}
			writeJSON(w, 200, out)
			return nil
		}))
	}
}
