// Package batch defines the Batch domain entity per CONTEXT.md: the
// aggregate "one API call to N recipients" unit. The on-the-wire payload
// is the proto SendTemplateReq; the storage row is sqlc.Message; the
// domain entity is batch.Batch.
package batch

import (
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/kannon-email/kannon/internal/tracking"
)

// Domain errors.
var (
	ErrBatchNotFound = errors.New("batch not found")

	// ErrTemplateMissing is a Batch that cannot be created because the Template
	// it names is not there.
	//
	// Intake looks the Template up before building the Batch, so in practice
	// this is the narrow race the lookup cannot close: the Template was deleted
	// in between. It is a refusal, not a fault — creating the Batch anyway is
	// what used to leave its Deliveries unbuildable (ADR 0008).
	ErrTemplateMissing = errors.New("batch template not found")
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

// OneClickUnsubscribe is the sender's own unsubscribe endpoint as stated for a
// Batch (CONTEXT.md, ADR 0005): an https URL template, personalised per
// Delivery, carried in the List-Unsubscribe and List-Unsubscribe-Post headers.
// Kannon emits and signs it and does nothing else with it.
//
// It is deliberately not a field of Headers. The To/Cc there are literal values
// written out as given, while this one is templated, percent-encoded and
// validated before it reaches a header line — sitting it beside them would
// suggest it is passed through as written.
type OneClickUnsubscribe struct {
	URLTemplate string
}

// IsZero reports whether the Batch states no unsubscribe endpoint, in which
// case neither header is emitted. Kannon never supplies one of its own.
func (u OneClickUnsubscribe) IsZero() bool { return u.URLTemplate == "" }

// validate checks what can be known before any Recipient is seen: that the
// template is a well-formed absolute https URL. Whether a given Recipient can
// actually resolve its placeholders is a per-Recipient question, answered at
// intake against that Recipient's fields.
//
// Only https qualifies. A one-click unsubscribe is a POST to an authenticated
// destination, so plain http would carry the recipient's identifier in the
// clear, and mailto is not one-click at all.
func (u OneClickUnsubscribe) validate() error {
	if u.IsZero() {
		return nil
	}
	// url.Parse rejects ASCII control characters, so this also covers the CR/LF
	// that would otherwise let a caller inject header lines of its own.
	parsed, err := url.Parse(u.URLTemplate)
	if err != nil {
		return fmt.Errorf("one-click unsubscribe URL is not a valid URL: %w", err)
	}
	if !strings.EqualFold(parsed.Scheme, "https") {
		return fmt.Errorf("one-click unsubscribe URL must be https, got %q", parsed.Scheme)
	}
	if parsed.Host == "" {
		return errors.New("one-click unsubscribe URL must be absolute")
	}
	return nil
}

// Attachments maps a filename to its raw bytes.
type Attachments map[string][]byte

// Batch is the aggregate created by one Mailer API call. It holds the
// metadata shared by all recipients of that call; per-recipient delivery
// state is tracked in the Delivery domain (see internal/delivery).
type Batch struct {
	id                  ID
	subject             string
	sender              Sender
	templateID          string
	domain              string
	attachments         Attachments
	headers             Headers
	oneClickUnsubscribe OneClickUnsubscribe
	tracking            tracking.Policy
}

// NewParams contains all fields needed to create a fresh Batch.
type NewParams struct {
	Domain      string
	Subject     string
	Sender      Sender
	TemplateID  string
	Attachments Attachments
	Headers     Headers
	// OneClickUnsubscribe is the sender's own unsubscribe endpoint. Zero when
	// the caller states none, in which case no unsubscribe header is emitted.
	OneClickUnsubscribe OneClickUnsubscribe
	// Tracking is the Tracking Policy as the caller stated it for this Batch —
	// persisted as provenance only (ADR 0003).
	Tracking tracking.Policy
}

// New creates a new Batch with a freshly generated ID for the given domain.
//
// The Tracking Policy is not normalised: an unstated Mode is kept exactly as
// stated, since the Batch column is the one place an unstated Mode may be
// stored. The value that actually governs each Delivery is resolved separately,
// against the Domain's ceiling, and frozen there instead.
func New(p NewParams) (*Batch, error) {
	if p.Domain == "" {
		return nil, errors.New("domain is required")
	}
	if p.Subject == "" {
		return nil, errors.New("subject is required")
	}
	if p.TemplateID == "" {
		return nil, errors.New("template ID is required")
	}
	if p.Sender.Email == "" {
		return nil, errors.New("sender email is required")
	}
	if err := p.OneClickUnsubscribe.validate(); err != nil {
		return nil, err
	}
	return &Batch{
		id:                  NewID(p.Domain),
		subject:             p.Subject,
		sender:              p.Sender,
		templateID:          p.TemplateID,
		domain:              p.Domain,
		attachments:         p.Attachments,
		headers:             p.Headers,
		oneClickUnsubscribe: p.OneClickUnsubscribe,
		tracking:            p.Tracking,
	}, nil
}

// LoadParams contains all fields needed to rehydrate a Batch from storage.
type LoadParams struct {
	ID                  ID
	Subject             string
	Sender              Sender
	TemplateID          string
	Domain              string
	Attachments         Attachments
	Headers             Headers
	OneClickUnsubscribe OneClickUnsubscribe
	Tracking            tracking.Policy
}

// Load rehydrates a Batch from stored data (used by repository implementations).
func Load(p LoadParams) *Batch {
	return &Batch{
		id:                  p.ID,
		subject:             p.Subject,
		sender:              p.Sender,
		templateID:          p.TemplateID,
		domain:              p.Domain,
		attachments:         p.Attachments,
		headers:             p.Headers,
		oneClickUnsubscribe: p.OneClickUnsubscribe,
		tracking:            p.Tracking,
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

// OneClickUnsubscribe is the sender's unsubscribe endpoint for this Batch, zero
// when none was stated.
func (b *Batch) OneClickUnsubscribe() OneClickUnsubscribe { return b.oneClickUnsubscribe }

// TrackingPolicy is the Tracking Policy as stated by the caller for this
// Batch — not the resolved value that governs any Delivery. It participates
// in resolution as the middle level of the cascade, between the Domain's
// ceiling and the Recipient (ADR 0003).
func (b *Batch) TrackingPolicy() tracking.Policy { return b.tracking }
