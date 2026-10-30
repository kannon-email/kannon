package intake

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
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
	ErrScheduledAtNotFuture        = errors.New("scheduled_at_not_future")
	ErrDuplicateAttachmentFilename = errors.New("duplicate_attachment_filename")
	ErrTooManyRecipients           = errors.New("too_many_recipients")
	ErrIdempotencyConflict         = errors.New("idempotency_conflict")
	ErrIdempotencyInProgress       = errors.New("idempotency_in_progress")
	ErrNoRecipientsAccepted        = errors.New("no_recipients_accepted")
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
}

func NewInlineService(store InlineStore, domains domains.Repository, backoff delivery.BackoffPolicy, retryWindow time.Duration) *InlineService {
	return &InlineService{store: store, domains: domains, backoff: backoff, retryWindow: retryWindow}
}
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
		if len(key) < 1 || len(key) > 256 || strings.TrimSpace(key) != key || strings.ContainsAny(key, "\r\n") {
			return nil, invalid(errors.New("invalid idempotency key"))
		}
		if len(p.Recipients) > 2000 {
			return nil, ErrTooManyRecipients
		}
		if p.Subject == "" || strings.ContainsAny(p.Subject+p.Sender.Alias, "\r\n") || len(p.Recipients) < 1 || (p.HTML == "") == (p.TemplateID == "") {
			return nil, invalid(errors.New("invalid or unsupported inline request"))
		}
		for _, list := range [][]string{p.Headers.To, p.Headers.Cc} {
			for _, address := range list {
				if strings.ContainsAny(address, "\r\n") {
					return nil, invalid(errors.New("invalid display header"))
				}
				if _, err := mail.ParseAddress(address); err != nil {
					return nil, invalid(errors.New("invalid display header"))
				}
			}
		}
		if _, reserved := p.GlobalFields["email"]; reserved {
			return nil, invalid(errors.New("email is reserved"))
		}
		// Fingerprint the full application input, including refused rows; retain only
		// its digest, never raw input or refusal details.
		raw, err := json.Marshal(p)
		if err != nil {
			return nil, err
		}
		sum := sha256.Sum256(raw)
		d, e := s.domains.FindByName(ctx, domain)
		if e != nil {
			return nil, e
		}
		var r *InlineReceipt
		r, replay, e = s.store.CreateInline(ctx, domain, key, hex.EncodeToString(sum[:]), func() (*InlineWork, error) {
			if !supportedPolicy(p.Tracking) || len(tracking.CeilingViolations(d.TrackingPolicy(), p.Tracking)) > 0 {
				return nil, invalid(errors.New("invalid batch tracking policy"))
			}
			now := time.Now().UTC().Truncate(time.Microsecond)
			if s.Now != nil {
				now = s.Now().UTC().Truncate(time.Microsecond)
			}
			scheduled := now
			if p.ScheduledAt != nil {
				scheduled = p.ScheduledAt.UTC().Truncate(time.Microsecond)
				if !scheduled.After(now) {
					return nil, ErrScheduledAtNotFuture
				}
			}
			attachments := batch.Attachments{}
			metadata := []AttachmentMetadata{}
			for _, a := range p.Attachments {
				if _, exists := attachments[a.Filename]; exists {
					return nil, ErrDuplicateAttachmentFilename
				}
				if a.Filename == "" || strings.ContainsAny(a.Filename, "\r\n") {
					return nil, invalid(errors.New("invalid attachment filename"))
				}
				attachments[a.Filename] = a.Content
				metadata = append(metadata, AttachmentMetadata{Filename: a.Filename, SizeBytes: len(a.Content)})
			}
			html := p.HTML
			if p.TemplateID != "" {
				source, err := s.store.FindByDomain(ctx, domain, p.TemplateID)
				if err != nil {
					return nil, err
				}
				html = source.Html()
			}
			tpl, e := templates.NewTransient(domain, html)
			if e != nil {
				return nil, invalid(e)
			}
			b, e := batch.New(batch.NewParams{Domain: domain.String(), Sender: p.Sender, Subject: p.Subject, TemplateID: tpl.TemplateID(), Attachments: attachments, Headers: p.Headers, Tracking: tracking.Resolve(d.TrackingPolicy(), p.Tracking, tracking.Policy{}), OneClickUnsubscribe: p.OneClickUnsubscribe})
			if e != nil {
				return nil, invalid(e)
			}
			ds := []*delivery.Delivery{}
			seen := map[string]bool{}
			for i, recipient := range p.Recipients {
				email := strings.TrimSpace(recipient.Email)
				if at := strings.LastIndex(email, "@"); at >= 0 {
					email = email[:at+1] + strings.ToLower(email[at+1:])
				}
				code := ""
				if !smtp.Validate(email) {
					code = "invalid_email"
				} else if seen[email] {
					code = "duplicate_recipient"
				}
				seen[email] = true
				fields := make(map[string]string)
				for k, v := range p.GlobalFields {
					fields[k] = v
				}
				for k, v := range recipient.Fields {
					fields[k] = v
				}
				if _, reserved := recipient.Fields["email"]; reserved && code == "" {
					code = "reserved_field"
				}
				fields["email"] = email
				if code == "" && (utils.HasUnresolvedPlaceholders(utils.ReplaceLiteralFields(html, fields)) || utils.HasUnresolvedPlaceholders(utils.ReplaceLiteralFields(p.Subject, fields))) {
					code = "unresolved_placeholder"
				}
				if strings.ContainsAny(utils.ReplaceLiteralFields(p.Subject, fields), "\r\n") && code == "" {
					code = "reserved_field"
				}
				recipientPolicy := recipient.Tracking
				if code == "" && !supportedPolicy(recipientPolicy) {
					code = string(ReasonUnsupportedTrackingMode)
				}
				if code == "" && (len(tracking.CeilingViolations(d.TrackingPolicy(), recipientPolicy)) > 0 || len(tracking.CeilingViolations(p.Tracking, recipientPolicy)) > 0) {
					code = string(ReasonTrackingAboveCeiling)
				}
				if _, ok := unresolvedUnsubscribeURL(p.OneClickUnsubscribe, batch.Recipient{Email: email, Fields: fields}); code == "" && !ok {
					code = string(ReasonUnsubscribeURLUnresolved)
				}
				if code != "" {
					refusals = append(refusals, InlineRefusal{Index: i, Code: code})
					continue
				}
				item, e := delivery.New(delivery.NewParams{BatchID: b.ID(), Domain: domain.String(), Email: email, Fields: fields, ScheduledTime: scheduled, Backoff: s.backoff, RetryWindow: s.retryWindow, Tracking: tracking.Resolve(d.TrackingPolicy(), p.Tracking, recipientPolicy)})
				if e != nil {
					return nil, e
				}
				ds = append(ds, item)
			}
			if len(ds) == 0 {
				return nil, ErrNoRecipientsAccepted
			}
			return &InlineWork{SourceTemplateID: p.TemplateID, Template: tpl, Batch: b, Deliveries: ds, Receipt: InlineReceipt{ID: b.ID().String(), Domain: domain.String(), CreatedAt: now, ScheduledAt: scheduled, Attachments: metadata, AcceptedCount: len(ds), RejectedCount: len(refusals)}}, nil
		})
		return r, e
	})
	return receipt, refusals, replay, err
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
