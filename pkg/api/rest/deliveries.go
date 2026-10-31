package rest

import (
	"net/http"

	"github.com/kannon-email/kannon/internal/intake"
)

func registerDeliveries(mux *http.ServeMux, service *intake.InlineService) {
	mux.Handle("GET /v1beta1/domains/{domain}/batches/{batch_id}/deliveries/{delivery_id}", handler(func(w http.ResponseWriter, r *http.Request) error {
		if _, err := parseQuery(r.URL.RawQuery); err != nil {
			return err
		}
		domain, err := parseDomainPath(r)
		if err != nil {
			return err
		}
		d, err := service.GetDelivery(r.Context(), domain, r.PathValue(batchIDParameter), r.PathValue("delivery_id"))
		if err != nil {
			return err
		}
		out := Delivery{Id: d.ID, BatchId: d.BatchID, Domain: d.Domain, Email: d.Email, Status: DeliveryStatus(d.Status), CreatedAt: d.CreatedAt, ScheduledAt: d.ScheduledAt, UpdatedAt: d.UpdatedAt, AttemptCount: d.AttemptCount, Tracking: TrackingPolicy{Opens: TrackingMode(d.Tracking.Opens), Links: TrackingMode(d.Tracking.Links)}}
		if d.ErrorCode != "" {
			out.LastError = &struct {
				Code   string `json:"code"`
				Detail string `json:"detail"`
			}{d.ErrorCode, d.ErrorDetail}
		}
		writeJSON(w, 200, out)
		return nil
	}))
}
