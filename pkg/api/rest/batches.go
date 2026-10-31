package rest

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/kannon-email/kannon/internal/intake"
)

func registerBatches(mux *http.ServeMux, service *intake.InlineService) {
	registerBatchHistory(mux, service)
	mux.Handle("POST /v1beta1/domains/{domain}/batches", handler(func(w http.ResponseWriter, r *http.Request) error {
		if _, err := parseQuery(r.URL.RawQuery); err != nil {
			return err
		}
		domain, err := parseDomainPath(r)
		if err != nil {
			return err
		}
		input, err := decodeBatch(r.Body)
		if err != nil {
			return err
		}
		key, err := idempotencyKey(r.Header)
		if err != nil {
			return err
		}
		receipt, refusals, replay, err := service.Create(r.Context(), domain, key, input.Request)
		rejected := []RecipientRejection{}
		for _, v := range refusals {
			field := "email"
			switch v.Code {
			case "tracking_above_ceiling", "unsupported_tracking_mode":
				field = "tracking"
			case "unsubscribe_url_unresolved", "unresolved_placeholder", "reserved_field":
				field = "fields"
			}
			rejected = append(rejected, RecipientRejection{Index: v.Index, Code: v.Code, Pointer: fmt.Sprintf("/recipients/%d/%s", v.Index, field), Detail: "Recipient refused: " + v.Code})
		}
		if err != nil {
			if errors.Is(err, intake.ErrNoRecipientsAccepted) {
				fields := make([]FieldError, 0, len(rejected))
				for _, v := range rejected {
					fields = append(fields, FieldError{Code: v.Code, Pointer: v.Pointer, Detail: v.Detail})
				}
				return &requestError{status: 422, code: "no_recipients_accepted", fields: fields}
			}
			return err
		}
		if replay {
			writeJSON(w, 200, BatchReplay{Id: receipt.ID, Domain: receipt.Domain, CreatedAt: receipt.CreatedAt, ScheduledAt: receipt.ScheduledAt, AcceptedCount: receipt.AcceptedCount, RejectedCount: receipt.RejectedCount})
			return nil
		}
		w.Header().Set("Location", "/v1beta1/domains/"+receipt.Domain+"/batches/"+receipt.ID)
		writeJSON(w, 201, BatchCreated{Id: receipt.ID, Domain: receipt.Domain, CreatedAt: receipt.CreatedAt, ScheduledAt: receipt.ScheduledAt, AcceptedCount: receipt.AcceptedCount, RejectedCount: receipt.RejectedCount, RejectedRecipients: rejected, From: input.From, Subject: input.Request.Subject, Status: BatchCreatedStatusPending, DeliveryCounts: DeliveryCounts{Pending: receipt.AcceptedCount}})
		return nil
	}))
}
