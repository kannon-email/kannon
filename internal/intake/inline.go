package intake

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"github.com/kannon-email/kannon/internal/apperr"
	"net/mail"
	"strings"
	"time"

	"github.com/kannon-email/kannon/internal/authz"
	"github.com/kannon-email/kannon/internal/batch"
	"github.com/kannon-email/kannon/internal/delivery"
	"github.com/kannon-email/kannon/internal/domains"
	smtp "github.com/kannon-email/kannon/internal/smtp"
	"github.com/kannon-email/kannon/internal/templates"
	"github.com/kannon-email/kannon/internal/tracking"
	"github.com/kannon-email/kannon/internal/utils"
	"github.com/kannon-email/kannon/internal/values"
)

var (
	ErrScheduledAtNotFuture        = apperr.New(apperr.Invalid, "scheduled_at_not_future", "")
	ErrDuplicateAttachmentFilename = apperr.New(apperr.Invalid, "duplicate_attachment_filename", "")
	ErrTooManyRecipients           = apperr.New(apperr.Invalid, "too_many_recipients", "")
	ErrIdempotencyConflict         = apperr.New(apperr.Conflict, "idempotency_conflict", "")
	ErrIdempotencyInProgress       = apperr.New(apperr.Conflict, "idempotency_in_progress", "")
	ErrNoRecipientsAccepted        = apperr.New(apperr.Invalid, "no_recipients_accepted", "")
)

// Attachment is one named file supplied with a Batch.
type Attachment struct {
	Filename string
	Content  []byte
}
type AttachmentMetadata struct {
	Filename  string
	SizeBytes int
}

// InlineRecipient keeps one target and its personalization and policy together.
type InlineRecipient struct {
	Email    string
	Fields   map[string]string
	Tracking tracking.Policy
}

// InlineRequest describes REST Batch content and recipients independently of transport.
type InlineRequest struct {
	Headers             batch.Headers
	Tracking            tracking.Policy
	OneClickUnsubscribe batch.OneClickUnsubscribe
	Sender              batch.Sender
	Subject             string
	HTML                string
	TemplateID          string
	GlobalFields        map[string]string
	Recipients          []InlineRecipient
	ScheduledAt         *time.Time   `json:",omitempty"`
	Attachments         []Attachment `json:",omitempty"`
}

// InlineReceipt contains only accepted identity and aggregate intake facts.
type InlineReceipt struct {
	ScheduledAt   time.Time
	Attachments   []AttachmentMetadata
	ID            string
	Domain        string
	CreatedAt     time.Time
	AcceptedCount int
	RejectedCount int
}

// InlineRefusal is response-only and must never enter persistence.
type InlineRefusal struct {
	Index int
	Code  string
}

// InlineWork is all accepted work to commit together.
type InlineWork struct {
	Template         *templates.Template
	Batch            *batch.Batch
	Deliveries       []*delivery.Delivery
	Receipt          InlineReceipt
	SourceTemplateID string
}

// InlineStore serializes a Domain/key pair without waiting on another intake.
// build is invoked only for a fresh key, inside the acceptance transaction.
type InlineStore interface {
	BatchReader
	// GetDelivery scopes the lookup to both its Domain and Batch.
	GetDelivery(ctx context.Context, domain values.DomainName, batchID, id string) (*DeliveryHistory, error)
	FindByDomain(context.Context, values.DomainName, string) (*templates.Template, error)
	CreateInline(context.Context, values.DomainName, string, string, func() (*InlineWork, error)) (*InlineReceipt, bool, error)
}

// InlineService owns REST intake semantics independently of transport types.
type InlineService struct {
	// Now supplies the creation clock; nil uses wall time.
	Now         func() time.Time
	store       InlineStore
	domains     domains.Repository
	backoff     delivery.BackoffPolicy
	retryWindow time.Duration
	// envelopeMaxBytes is the largest Envelope the transport carries; zero leaves the size unchecked.
	envelopeMaxBytes int
}

func NewInlineService(store InlineStore, domains domains.Repository, backoff delivery.BackoffPolicy, retryWindow time.Duration, envelopeMaxBytes int) *InlineService {
	return &InlineService{store: store, domains: domains, backoff: backoff, retryWindow: retryWindow, envelopeMaxBytes: envelopeMaxBytes}
}

// Create authorizes, then accepts a REST Batch once per Domain/key pair. Request
// rules that hold whenever they are asked are checked before the idempotency
// lookup; the rules whose answer can change with time or state (schedule, Domain
// ceiling, Templates) run in buildWork, only for a fresh key, so a replay returns
// its retained receipt. A replay reports no refusals: idempotency retains the
// creation receipt, never refusal details (ADR 0016).
func (s *InlineService) Create(ctx context.Context, domain values.DomainName, key string, p InlineRequest) (*InlineReceipt, []InlineRefusal, bool, error) {
	var refusals []InlineRefusal
	var replay bool
	host, err := smtp.GetEmailDomain(p.Sender.Email)
	if err != nil {
		return nil, nil, false, invalid(err)
	}
	var constraint error
	if !senderHostPermitted(canonicalSenderHost(host), domain) {
		constraint = authz.ErrForbidden
	}
	receipt, err := authz.GuardWithConstraint(ctx, authz.Create, authz.Batches(domain), constraint, func() (*InlineReceipt, error) {
		if err := validateInline(key, p); err != nil {
			return nil, err
		}
		fingerprint, err := inlineFingerprint(p)
		if err != nil {
			return nil, err
		}
		d, err := s.domains.FindByName(ctx, domain)
		if err != nil {
			return nil, err
		}
		var r *InlineReceipt
		r, replay, err = s.store.CreateInline(ctx, domain, key, fingerprint, func() (*InlineWork, error) {
			work, rs, err := s.buildWork(ctx, domain, d.TrackingPolicy(), p)
			refusals = rs
			return work, err
		})
		return r, err
	})
	return receipt, refusals, replay, err
}

// validateInline applies the request rules that do not depend on time or stored
// state, in the order their faults are reported.
func validateInline(key string, p InlineRequest) error {
	if len(key) < 1 || len(key) > 256 || strings.TrimSpace(key) != key || strings.ContainsAny(key, "\r\n") {
		return invalid(errors.New("invalid idempotency key"))
	}
	if len(p.Recipients) > maxInlineRecipients {
		return ErrTooManyRecipients
	}
	if p.Subject == "" || strings.ContainsAny(p.Subject+p.Sender.Alias, "\r\n") || len(p.Recipients) < 1 || (p.HTML == "") == (p.TemplateID == "") {
		return invalid(errors.New("invalid or unsupported inline request"))
	}
	for _, list := range [][]string{p.Headers.To, p.Headers.Cc} {
		for _, address := range list {
			if strings.ContainsAny(address, "\r\n") {
				return invalid(errors.New("invalid display header"))
			}
			if _, err := mail.ParseAddress(address); err != nil {
				return invalid(errors.New("invalid display header"))
			}
		}
	}
	if _, reserved := p.GlobalFields["email"]; reserved {
		return invalid(errors.New("email is reserved"))
	}
	return nil
}

// maxInlineRecipients bounds one REST Batch.
const maxInlineRecipients = 2000

// inlineFingerprint digests the full application input, including refused rows;
// only the digest is retained, never raw input or refusal details.
func inlineFingerprint(p InlineRequest) (string, error) {
	raw, err := json.Marshal(p)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}

// now is the creation clock at the precision persistence retains.
func (s *InlineService) now() time.Time {
	if s.Now != nil {
		return s.Now().UTC().Truncate(time.Microsecond)
	}
	return time.Now().UTC().Truncate(time.Microsecond)
}

// buildWork decides a fresh key's Batch: everything here runs inside the
// acceptance transaction and only when no receipt exists for the key.
func (s *InlineService) buildWork(ctx context.Context, domain values.DomainName, ceiling tracking.Policy, p InlineRequest) (*InlineWork, []InlineRefusal, error) {
	if !supportedPolicy(p.Tracking) || len(tracking.CeilingViolations(ceiling, p.Tracking)) > 0 {
		return nil, nil, invalid(errors.New("invalid batch tracking policy"))
	}
	now := s.now()
	scheduled, err := inlineSchedule(p.ScheduledAt, now)
	if err != nil {
		return nil, nil, err
	}
	attachments, metadata, err := inlineAttachments(p.Attachments)
	if err != nil {
		return nil, nil, err
	}
	html := p.HTML
	if p.TemplateID != "" {
		source, err := s.store.FindByDomain(ctx, domain, p.TemplateID)
		if err != nil {
			return nil, nil, err
		}
		html = source.Html()
	}
	if err := checkMessageSize(s.envelopeMaxBytes, html, attachments); err != nil {
		return nil, nil, err
	}
	tpl, err := templates.NewTransient(domain, html)
	if err != nil {
		return nil, nil, invalid(err)
	}
	b, err := batch.New(batch.NewParams{Domain: domain.String(), Sender: p.Sender, Subject: p.Subject, TemplateID: tpl.TemplateID(), Attachments: attachments, Headers: p.Headers, Tracking: tracking.Resolve(ceiling, p.Tracking, tracking.Policy{}), OneClickUnsubscribe: p.OneClickUnsubscribe})
	if err != nil {
		return nil, nil, invalid(err)
	}
	accepted, refusals := decideRecipients(ceiling, html, p)
	ds := []*delivery.Delivery{}
	for _, r := range accepted {
		// An immediate Batch is scheduled at its receipt time, so its Deliveries
		// are pending from the moment the receipt exists.
		item, err := delivery.New(delivery.NewParams{BatchID: b.ID(), Domain: domain.String(), Email: r.email, Fields: r.fields, ScheduledTime: scheduled, Backoff: s.backoff, RetryWindow: s.retryWindow, Tracking: r.tracking})
		if err != nil {
			return nil, refusals, err
		}
		ds = append(ds, item)
	}
	// REST differs from legacy Accept here: a Batch with no accepted Recipient
	// is refused whole rather than created empty.
	if len(ds) == 0 {
		return nil, refusals, ErrNoRecipientsAccepted
	}
	return &InlineWork{SourceTemplateID: p.TemplateID, Template: tpl, Batch: b, Deliveries: ds, Receipt: InlineReceipt{ID: b.ID().String(), Domain: domain.String(), CreatedAt: now, ScheduledAt: scheduled, Attachments: metadata, AcceptedCount: len(ds), RejectedCount: len(refusals)}}, refusals, nil
}

// inlineSchedule resolves when a Batch's Deliveries start: now when unstated.
// REST differs from legacy Accept here: a stated time must be in the future.
func inlineSchedule(at *time.Time, now time.Time) (time.Time, error) {
	if at == nil {
		return now, nil
	}
	scheduled := at.UTC().Truncate(time.Microsecond)
	if !scheduled.After(now) {
		return time.Time{}, ErrScheduledAtNotFuture
	}
	return scheduled, nil
}

// inlineAttachments keys attachments by filename and keeps their request order
// in the metadata a receipt retains.
func inlineAttachments(as []Attachment) (batch.Attachments, []AttachmentMetadata, error) {
	attachments := batch.Attachments{}
	metadata := []AttachmentMetadata{}
	for _, a := range as {
		if _, exists := attachments[a.Filename]; exists {
			return nil, nil, ErrDuplicateAttachmentFilename
		}
		if a.Filename == "" || strings.ContainsAny(a.Filename, "\r\n") {
			return nil, nil, invalid(errors.New("invalid attachment filename"))
		}
		attachments[a.Filename] = a.Content
		metadata = append(metadata, AttachmentMetadata{Filename: a.Filename, SizeBytes: len(a.Content)})
	}
	return attachments, metadata, nil
}

// REST-only Recipient refusals. They are wire codes of the REST contract
// (x-recipient-rejection-codes), beside the reasons REST shares with legacy intake.
const (
	ReasonDuplicateRecipient    RejectionReason = "duplicate_recipient"
	ReasonUnresolvedPlaceholder RejectionReason = "unresolved_placeholder"
	ReasonReservedField         RejectionReason = "reserved_field"
)

// inlineRecipient is one accepted Recipient, ready to become a Delivery.
type inlineRecipient struct {
	email    string
	fields   map[string]string
	tracking tracking.Policy
}

// decideRecipients accepts or refuses each Recipient independently, preserving
// request order. html is the Batch body before personalization.
func decideRecipients(ceiling tracking.Policy, html string, p InlineRequest) ([]inlineRecipient, []InlineRefusal) {
	var accepted []inlineRecipient
	var refusals []InlineRefusal
	seen := map[string]bool{}
	for i, recipient := range p.Recipients {
		r, reason := decideRecipient(ceiling, html, p, recipient, seen)
		if reason != "" {
			refusals = append(refusals, InlineRefusal{Index: i, Code: string(reason)})
			continue
		}
		accepted = append(accepted, r)
	}
	return accepted, refusals
}

// decideRecipient returns the first applicable refusal, in the precedence the
// checks are listed. seen records every normalized address, refused or not.
//
// REST differs from legacy Accept here: global and Recipient fields are merged
// and substituted literally, once, and a placeholder left unresolved refuses the
// Recipient instead of being sent verbatim.
func decideRecipient(ceiling tracking.Policy, html string, p InlineRequest, recipient InlineRecipient, seen map[string]bool) (inlineRecipient, RejectionReason) {
	email := normalizeInlineEmail(recipient.Email)
	var reason RejectionReason
	if !smtp.Validate(email) {
		reason = ReasonInvalidEmail
	} else if seen[email] {
		reason = ReasonDuplicateRecipient
	}
	seen[email] = true
	fields := make(map[string]string)
	for k, v := range p.GlobalFields {
		fields[k] = v
	}
	for k, v := range recipient.Fields {
		fields[k] = v
	}
	if _, reserved := recipient.Fields["email"]; reserved && reason == "" {
		reason = ReasonReservedField
	}
	fields["email"] = email
	subject := utils.ReplaceLiteralFields(p.Subject, fields)
	if reason == "" && (utils.HasUnresolvedPlaceholders(utils.ReplaceLiteralFields(html, fields)) || utils.HasUnresolvedPlaceholders(subject)) {
		reason = ReasonUnresolvedPlaceholder
	}
	if reason == "" && strings.ContainsAny(subject, "\r\n") {
		reason = ReasonReservedField
	}
	if reason == "" && !supportedPolicy(recipient.Tracking) {
		reason = ReasonUnsupportedTrackingMode
	}
	if reason == "" && (len(tracking.CeilingViolations(ceiling, recipient.Tracking)) > 0 || len(tracking.CeilingViolations(p.Tracking, recipient.Tracking)) > 0) {
		reason = ReasonTrackingAboveCeiling
	}
	if _, ok := unresolvedUnsubscribeURL(p.OneClickUnsubscribe, batch.Recipient{Email: email, Fields: fields}); reason == "" && !ok {
		reason = ReasonUnsubscribeURLUnresolved
	}
	if reason != "" {
		return inlineRecipient{}, reason
	}
	return inlineRecipient{email: email, fields: fields, tracking: tracking.Resolve(ceiling, p.Tracking, recipient.Tracking)}, ""
}

// normalizeInlineEmail trims an address and lowercases its domain part only;
// the local part is case-sensitive.
func normalizeInlineEmail(email string) string {
	email = strings.TrimSpace(email)
	if at := strings.LastIndex(email, "@"); at >= 0 {
		email = email[:at+1] + strings.ToLower(email[at+1:])
	}
	return email
}

// supportedPolicy distinguishes silence from modes this build cannot execute.
func supportedPolicy(p tracking.Policy) bool {
	for _, m := range []tracking.Mode{p.Opens, p.Links} {
		if m != tracking.ModeUnspecified {
			if _, ok := m.Rank(); !ok {
				return false
			}
		}
	}
	return true
}
