package rest

import (
	"errors"
	"fmt"
	"github.com/kannon-email/kannon/internal/batch"
	"github.com/kannon-email/kannon/internal/intake"
	"github.com/kannon-email/kannon/internal/tracking"
	"github.com/kannon-email/kannon/internal/values"
	"net/http"
	"time"
)

func registerBatches(mux *http.ServeMux, service *intake.InlineService) {
	registerBatchHistory(mux, service)
	mux.Handle("POST /v1beta1/domains/{domain}/batches", handler(func(w http.ResponseWriter, r *http.Request) error {
		if err := query(r, false); err != nil {
			return err
		}
		domain, err := values.Parse(r.PathValue(domainParameter))
		if err != nil {
			return problemError(400, "malformed_request")
		}
		// A strict supported subset ensures optional extensions cannot be silently
		// discarded, including unknown members inside the generated content union.
		var input struct {
			Headers             *Headers        `json:"headers"`
			Tracking            tracking.Policy `json:"tracking"`
			OneClickUnsubscribe *struct {
				URLTemplate *string `json:"url_template"`
			} `json:"one_click_unsubscribe"`
			ScheduledAt *time.Time `json:"scheduled_at"`
			Attachments []struct {
				Filename string  `json:"filename"`
				Content  *[]byte `json:"content_base64"`
			} `json:"attachments"`
			From    From   `json:"from"`
			Subject string `json:"subject"`
			Content struct {
				Type       string  `json:"type"`
				HTML       *string `json:"html"`
				TemplateID *string `json:"template_id"`
			} `json:"content"`
			GlobalFields map[string]string `json:"global_fields"`
			Recipients   []struct {
				Tracking tracking.Policy   `json:"tracking"`
				Fields   map[string]string `json:"fields"`
				Email    *string           `json:"email"`
			} `json:"recipients"`
		}
		if err := decode(w, r, &input); err != nil {
			return err
		}
		validHTML := input.Content.Type == "html" && input.Content.HTML != nil && *input.Content.HTML != "" && input.Content.TemplateID == nil
		validTemplate := input.Content.Type == "template" && input.Content.TemplateID != nil && *input.Content.TemplateID != "" && input.Content.HTML == nil
		if !validHTML && !validTemplate {
			return problemError(422, "validation_failed")
		}
		p := intake.InlineRequest{ScheduledAt: input.ScheduledAt, Sender: batch.Sender{Email: string(input.From.Email)}, Subject: input.Subject, GlobalFields: input.GlobalFields}
		p.Tracking = input.Tracking
		if input.Headers != nil {
			if input.Headers.To != nil {
				p.Headers.To = *input.Headers.To
			}
			if input.Headers.Cc != nil {
				p.Headers.Cc = *input.Headers.Cc
			}
		}
		if input.OneClickUnsubscribe != nil {
			if input.OneClickUnsubscribe.URLTemplate == nil || *input.OneClickUnsubscribe.URLTemplate == "" {
				return problemError(422, "validation_failed")
			}
			p.OneClickUnsubscribe.URLTemplate = *input.OneClickUnsubscribe.URLTemplate
		}
		if input.Content.HTML != nil {
			p.HTML = *input.Content.HTML
		}
		if input.Content.TemplateID != nil {
			p.TemplateID = *input.Content.TemplateID
		}
		for _, a := range input.Attachments {
			if a.Content == nil {
				return problemError(422, "validation_failed")
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
			p.Recipients = append(p.Recipients, intake.InlineRecipient{Email: email, Fields: v.Fields, Tracking: v.Tracking})
		}
		keys := r.Header.Values("Idempotency-Key")
		if len(keys) != 1 || keys[0] == "" {
			return problemError(400, "malformed_request")
		}
		receipt, refusals, replay, err := service.Create(r.Context(), domain, keys[0], p)
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
		writeJSON(w, 201, BatchCreated{Id: receipt.ID, Domain: receipt.Domain, CreatedAt: receipt.CreatedAt, ScheduledAt: receipt.ScheduledAt, AcceptedCount: receipt.AcceptedCount, RejectedCount: receipt.RejectedCount, RejectedRecipients: rejected, From: input.From, Subject: input.Subject, Status: BatchCreatedStatusPending, DeliveryCounts: DeliveryCounts{Pending: receipt.AcceptedCount}})
		return nil
	}))
}
