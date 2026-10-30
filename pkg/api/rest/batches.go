package rest

import (
	"errors"
	"fmt"
	"github.com/kannon-email/kannon/internal/batch"
	"github.com/kannon-email/kannon/internal/intake"
	"github.com/kannon-email/kannon/internal/values"
	"net/http"
	"time"
)

func registerBatches(mux *http.ServeMux, service *intake.InlineService) {
	registerBatchHistory(mux, service)
	mux.HandleFunc("POST /v1beta1/domains/{domain}/batches", func(w http.ResponseWriter, r *http.Request) {
		if !query(w, r, false) {
			return
		}
		domain, err := values.Parse(r.PathValue("domain"))
		if err != nil {
			WriteProblem(w, r, 400, "malformed_request")
			return
		}
		// A strict supported subset ensures optional extensions cannot be silently
		// discarded, including unknown members inside the generated content union.
		var input struct {
			ScheduledAt *time.Time `json:"scheduled_at"`
			Attachments []struct {
				Filename string  `json:"filename"`
				Content  *[]byte `json:"content_base64"`
			} `json:"attachments"`
			From    From   `json:"from"`
			Subject string `json:"subject"`
			Content struct {
				Type string  `json:"type"`
				HTML *string `json:"html"`
			} `json:"content"`
			Recipients []struct {
				Email *string `json:"email"`
			} `json:"recipients"`
		}
		if !decode(w, r, &input) {
			return
		}
		if input.Content.Type != "html" || input.Content.HTML == nil {
			WriteProblem(w, r, 422, "validation_failed")
			return
		}
		p := intake.InlineRequest{ScheduledAt: input.ScheduledAt, Sender: batch.Sender{Email: string(input.From.Email)}, Subject: input.Subject, HTML: *input.Content.HTML}
		for _, a := range input.Attachments {
			if a.Content == nil {
				WriteProblem(w, r, 422, "validation_failed")
				return
			}
			p.Attachments = append(p.Attachments, intake.Attachment{Filename: a.Filename, Content: *a.Content})
		}
		if input.From.Name != nil {
			p.Sender.Alias = *input.From.Name
		}
		for _, v := range input.Recipients {
			email := ""
			if v.Email != nil {
				email = *v.Email
			}
			p.Recipients = append(p.Recipients, email)
		}
		keys := r.Header.Values("Idempotency-Key")
		if len(keys) != 1 || keys[0] == "" {
			WriteProblem(w, r, 400, "malformed_request")
			return
		}
		receipt, refusals, replay, err := service.Create(r.Context(), domain, keys[0], p)
		if err != nil {
			var invalid *intake.InvalidRequest
			switch {
			case errors.Is(err, intake.ErrIdempotencyConflict):
				WriteProblem(w, r, 409, "idempotency_conflict")
			case errors.Is(err, intake.ErrIdempotencyInProgress):
				w.Header().Set("Retry-After", "1")
				WriteProblem(w, r, 409, "idempotency_in_progress")
			case errors.Is(err, intake.ErrScheduledAtNotFuture):
				WriteProblem(w, r, 422, "scheduled_at_not_future")
			case errors.Is(err, intake.ErrDuplicateAttachmentFilename):
				WriteProblem(w, r, 422, "duplicate_attachment_filename")
			case errors.Is(err, intake.ErrTooManyRecipients):
				WriteProblem(w, r, 422, "too_many_recipients")
			case errors.Is(err, intake.ErrNoRecipientsAccepted):
				WriteProblem(w, r, 422, "no_recipients_accepted")
			case errors.As(err, &invalid):
				WriteProblem(w, r, 422, "validation_failed")
			default:
				writeError(w, r, err)
			}
			return
		}
		if replay {
			writeJSON(w, 200, BatchReplay{Id: receipt.ID, Domain: receipt.Domain, CreatedAt: receipt.CreatedAt, ScheduledAt: receipt.ScheduledAt, AcceptedCount: receipt.AcceptedCount, RejectedCount: receipt.RejectedCount})
			return
		}
		rejected := []RecipientRejection{}
		for _, v := range refusals {
			rejected = append(rejected, RecipientRejection{Index: v.Index, Code: v.Code, Pointer: fmt.Sprintf("/recipients/%d/email", v.Index), Detail: "Recipient refused: " + v.Code})
		}
		w.Header().Set("Location", "/v1beta1/domains/"+receipt.Domain+"/batches/"+receipt.ID)
		writeJSON(w, 201, BatchCreated{Id: receipt.ID, Domain: receipt.Domain, CreatedAt: receipt.CreatedAt, ScheduledAt: receipt.ScheduledAt, AcceptedCount: receipt.AcceptedCount, RejectedCount: receipt.RejectedCount, RejectedRecipients: rejected, From: input.From, Subject: input.Subject, Status: BatchCreatedStatusPending, DeliveryCounts: DeliveryCounts{Pending: receipt.AcceptedCount}})
	})
}
