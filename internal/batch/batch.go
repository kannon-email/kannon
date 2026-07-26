// Package batch defines the Batch domain entity per CONTEXT.md: the
// aggregate "one API call to N recipients" unit. The on-the-wire payload
// is the proto SendTemplateReq; the storage row is sqlc.Message; the
// domain entity is batch.Batch.
package batch

import (
	"errors"

	"github.com/kannon-email/kannon/internal/tracking"
)

// Domain errors.
var (
	ErrBatchNotFound = errors.New("batch not found")
)

// Sender is the visible from-identity of a Batch (display alias + email).
type Sender struct {
	Email string
	Alias string
}

// Headers carries optional custom To/Cc lists rendered into the outgoing
// envelope. The recipients listed here are header-only; they do not drive
// per-recipient delivery scheduling.
type Headers struct {
	To []string
	Cc []string
}

// Attachments maps a filename to its raw bytes.
type Attachments map[string][]byte

// Batch is the aggregate created by one Mailer API call. It holds the
// metadata shared by all recipients of that call; per-recipient delivery
// state is tracked in the Delivery domain (see internal/delivery).
type Batch struct {
	id          ID
	subject     string
	sender      Sender
	templateID  string
	domain      string
	attachments Attachments
	headers     Headers
	tracking    tracking.Policy
}

// New creates a new Batch with a freshly generated ID for the given domain.
// trackingPolicy is the Tracking Policy as the caller stated it for this
// Batch — persisted as provenance only (ADR 0003). It is not normalised: an
// unstated Mode is kept exactly as stated, since the Batch column is the one
// place an unstated Mode may be stored. The value that actually governs each
// Delivery is resolved separately, against the Domain's ceiling, and frozen
// there instead.
func New(domain, subject string, sender Sender, templateID string, attachments Attachments, headers Headers, trackingPolicy tracking.Policy) (*Batch, error) {
	if domain == "" {
		return nil, errors.New("domain is required")
	}
	if subject == "" {
		return nil, errors.New("subject is required")
	}
	if templateID == "" {
		return nil, errors.New("template ID is required")
	}
	if sender.Email == "" {
		return nil, errors.New("sender email is required")
	}
	return &Batch{
		id:          NewID(domain),
		subject:     subject,
		sender:      sender,
		templateID:  templateID,
		domain:      domain,
		attachments: attachments,
		headers:     headers,
		tracking:    trackingPolicy,
	}, nil
}

// LoadParams contains all fields needed to rehydrate a Batch from storage.
type LoadParams struct {
	ID          ID
	Subject     string
	Sender      Sender
	TemplateID  string
	Domain      string
	Attachments Attachments
	Headers     Headers
	Tracking    tracking.Policy
}

// Load rehydrates a Batch from stored data (used by repository implementations).
func Load(p LoadParams) *Batch {
	return &Batch{
		id:          p.ID,
		subject:     p.Subject,
		sender:      p.Sender,
		templateID:  p.TemplateID,
		domain:      p.Domain,
		attachments: p.Attachments,
		headers:     p.Headers,
		tracking:    p.Tracking,
	}
}

// Getters

func (b *Batch) ID() ID                   { return b.id }
func (b *Batch) Subject() string          { return b.subject }
func (b *Batch) Sender() Sender           { return b.sender }
func (b *Batch) TemplateID() string       { return b.templateID }
func (b *Batch) Domain() string           { return b.domain }
func (b *Batch) Attachments() Attachments { return b.attachments }
func (b *Batch) Headers() Headers         { return b.headers }

// TrackingPolicy is the Tracking Policy as stated by the caller for this
// Batch — not the resolved value that governs any Delivery. It participates
// in resolution as the middle level of the cascade, between the Domain's
// ceiling and the Recipient (ADR 0003).
func (b *Batch) TrackingPolicy() tracking.Policy { return b.tracking }
