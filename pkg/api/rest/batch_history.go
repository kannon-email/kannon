package rest

import (
	"encoding/base64"
	"encoding/json"
	"errors"
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
		mux.HandleFunc("GET "+path, func(w http.ResponseWriter, r *http.Request) {
			d, err := values.Parse(r.PathValue("domain"))
			if err != nil {
				WriteProblem(w, r, 400, "malformed_request")
				return
			}
			id := r.PathValue("batch_id")
			deliveries := r.Pattern == "GET /v1beta1/domains/{domain}/batches/{batch_id}/deliveries"
			detail := id != "" && !deliveries
			q := intake.HistoryQuery{Limit: 100}
			v := r.URL.Query()
			for k, vs := range v {
				if len(vs) != 1 || detail || (k != "limit" && k != "cursor" && !(deliveries && k == "status")) {
					WriteProblem(w, r, 400, "invalid_query_parameter")
					return
				}
			}
			if v.Has("limit") {
				q.Limit, err = strconv.Atoi(v.Get("limit"))
				if err != nil || q.Limit < 1 || q.Limit > 1000 {
					WriteProblem(w, r, 400, "invalid_query_parameter")
					return
				}
			}
			q.Status = v.Get("status")
			if v.Has("status") {
				switch q.Status {
				case "pending", "scheduled", "sending", "delivered", "rejected", "bounced", "failed":
				default:
					WriteProblem(w, r, 400, "invalid_query_parameter")
					return
				}
			}
			scope := d.String() + ":" + id + ":" + strconv.FormatBool(deliveries) + ":" + q.Status
			if v.Has("cursor") {
				raw, e := base64.RawURLEncoding.DecodeString(v.Get("cursor"))
				var c historyCursor
				if e != nil || json.Unmarshal(raw, &c) != nil || c.Scope != scope || c.ID == "" || (!deliveries && c.Time.IsZero()) {
					WriteProblem(w, r, 400, "invalid_cursor")
					return
				}
				q.AfterID = c.ID
				q.AfterTime = c.Time
			}
			fail := func(err error) {
				if errors.Is(err, intake.ErrDeliveryNotFound) {
					WriteProblem(w, r, 404, "not_found")
				} else {
					writeError(w, r, err)
				}
			}
			if detail {
				b, e := s.ReadBatch(r.Context(), d, id)
				if e != nil {
					fail(e)
					return
				}
				a := batchSummary(*b)
				out := Batch{Id: a.Id, Domain: a.Domain, From: a.From, Subject: a.Subject, CreatedAt: a.CreatedAt, ScheduledAt: a.ScheduledAt, CompletedAt: a.CompletedAt, ExpiresAt: a.ExpiresAt, AcceptedCount: a.AcceptedCount, RejectedCount: a.RejectedCount, DeliveryCounts: a.DeliveryCounts, Status: BatchStatus(a.Status), Tracking: TrackingPolicy{Opens: TrackingMode(b.Tracking.Opens), Links: TrackingMode(b.Tracking.Links)}, Attachments: []AttachmentMetadata{}}
				out.Content.Html = b.HTML
				out.Content.Type = "html"
				for _, att := range b.Attachments {
					out.Attachments = append(out.Attachments, AttachmentMetadata{Filename: att.Filename, SizeBytes: att.SizeBytes})
				}
				writeJSON(w, 200, out)
				return
			}
			limit := q.Limit
			q.Limit++
			cursor := func(id string, t time.Time) *string {
				raw, _ := json.Marshal(historyCursor{scope, id, t})
				v := base64.RawURLEncoding.EncodeToString(raw)
				return &v
			}
			if deliveries {
				rows, e := s.ListDeliveries(r.Context(), d, id, q)
				if e != nil {
					fail(e)
					return
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
				return
			}
			rows, e := s.ListBatches(r.Context(), d, q)
			if e != nil {
				fail(e)
				return
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
		})
	}
}
