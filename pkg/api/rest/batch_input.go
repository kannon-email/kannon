package rest

import (
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"

	"github.com/kannon-email/kannon/internal/batch"
	"github.com/kannon-email/kannon/internal/intake"
	"github.com/kannon-email/kannon/internal/tracking"
)

type decodedBatch struct {
	Request intake.InlineRequest
	From    From
}

func decodeBatch(body io.Reader) (decodedBatch, error) {
	input, raw, err := decodeBodyBytes[BatchCreate](body)
	if err != nil {
		return decodedBatch{}, err
	}
	html, templateID, err := decodeContent(input.Content)
	if err != nil {
		return decodedBatch{}, err
	}
	p := intake.InlineRequest{HTML: html, TemplateID: templateID, ScheduledAt: input.ScheduledAt, Sender: batch.Sender{Email: string(input.From.Email)}, Subject: input.Subject}
	if input.GlobalFields != nil {
		p.GlobalFields = *input.GlobalFields
	}
	if input.Tracking != nil {
		if input.Tracking.Opens != nil {
			p.Tracking.Opens = tracking.Mode(*input.Tracking.Opens)
		}
		if input.Tracking.Links != nil {
			p.Tracking.Links = tracking.Mode(*input.Tracking.Links)
		}
	}
	if input.Headers != nil {
		if input.Headers.To != nil {
			p.Headers.To = *input.Headers.To
		}
		if input.Headers.Cc != nil {
			p.Headers.Cc = *input.Headers.Cc
		}
	}
	if input.OneClickUnsubscribe != nil {
		if input.OneClickUnsubscribe.UrlTemplate == "" {
			return decodedBatch{}, problemError(422, "validation_failed")
		}
		p.OneClickUnsubscribe.URLTemplate = input.OneClickUnsubscribe.UrlTemplate
	}
	if input.ReplyTo != nil {
		// A stated reply_to without its email would decode to no Reply-To at all.
		if input.ReplyTo.Email == "" {
			return decodedBatch{}, problemError(422, "validation_failed")
		}
		p.ReplyTo.Email = string(input.ReplyTo.Email)
		if input.ReplyTo.Name != nil {
			p.ReplyTo.Alias = *input.ReplyTo.Name
		}
	}
	p.Attachments, err = decodeAttachments(input.Attachments, raw)
	if err != nil {
		return decodedBatch{}, err
	}
	if input.From.Name != nil {
		p.Sender.Alias = *input.From.Name
	}
	for _, v := range input.Recipients {
		recipient := intake.InlineRecipient{}
		if v.Email != nil {
			recipient.Email = *v.Email
		}
		if v.Fields != nil {
			recipient.Fields = *v.Fields
		}
		if v.Tracking != nil {
			if v.Tracking.Opens != nil {
				recipient.Tracking.Opens = tracking.Mode(*v.Tracking.Opens)
			}
			if v.Tracking.Links != nil {
				recipient.Tracking.Links = tracking.Mode(*v.Tracking.Links)
			}
		}
		p.Recipients = append(p.Recipients, recipient)
	}
	return decodedBatch{Request: p, From: input.From}, nil
}

// The generated union preserves raw JSON but does not validate its members.
// A strict superset preserves the 422 response for both alternatives together.
func decodeContent(input ContentInput) (string, string, error) {
	raw, err := input.MarshalJSON()
	if err != nil {
		return "", "", problemError(400, "malformed_request")
	}
	var content struct {
		Type       string  `json:"type"`
		HTML       *string `json:"html"`
		TemplateID *string `json:"template_id"`
	}
	// Omitted content reaches required-content validation, as before.
	if len(raw) != 0 {
		if err := strictJSON(raw, &content); err != nil {
			return "", "", err
		}
	}
	if content.Type == "html" && content.HTML != nil && *content.HTML != "" && content.TemplateID == nil {
		return *content.HTML, "", nil
	}
	if content.Type == "template" && content.TemplateID != nil && *content.TemplateID != "" && content.HTML == nil {
		return "", *content.TemplateID, nil
	}
	return "", "", problemError(422, "validation_failed")
}

func decodeAttachments(input *[]AttachmentInput, raw []byte) ([]intake.Attachment, error) {
	if input == nil {
		return nil, nil
	}
	// Empty bytes are valid; only this member needs an independent presence check.
	var presence struct {
		Attachments []struct {
			Content *json.RawMessage `json:"content_base64"`
		} `json:"attachments"`
	}
	if err := json.Unmarshal(raw, &presence); err != nil {
		return nil, problemError(400, "malformed_request")
	}
	var attachments []intake.Attachment
	for i, a := range *input {
		if presence.Attachments[i].Content == nil {
			return nil, problemError(422, "validation_failed")
		}
		content, err := base64.StdEncoding.DecodeString(a.ContentBase64)
		if err != nil {
			return nil, problemError(400, "malformed_request")
		}
		attachments = append(attachments, intake.Attachment{Filename: a.Filename, Content: content})
	}
	return attachments, nil
}

func idempotencyKey(header http.Header) (string, error) {
	keys := header.Values("Idempotency-Key")
	if len(keys) != 1 || keys[0] == "" {
		return "", problemError(400, "malformed_request")
	}
	return keys[0], nil
}
