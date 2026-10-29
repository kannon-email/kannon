// Package intake authorizes and atomically accepts a Batch and its Deliveries.
package intake

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/kannon-email/kannon/internal/authz"
	"github.com/kannon-email/kannon/internal/batch"
	"github.com/kannon-email/kannon/internal/delivery"
	"github.com/kannon-email/kannon/internal/domains"
	smtputils "github.com/kannon-email/kannon/internal/smtp"
	"github.com/kannon-email/kannon/internal/templates"
	"github.com/kannon-email/kannon/internal/tracking"
	"github.com/kannon-email/kannon/internal/utils"
	"github.com/kannon-email/kannon/internal/values"
)

// Store persists one acceptance atomically. A nil Template means reuse the
// existing Template; any failure must leave all three kinds of records unchanged.
type Store interface {
	FindByDomain(context.Context, values.DomainName, string) (*templates.Template, error)
	Accept(context.Context, *templates.Template, *batch.Batch, []*delivery.Delivery) error
}

// Service owns Batch intake, including authorization and the Retry Budget.
type Service struct {
	store       Store
	backoff     delivery.BackoffPolicy
	retryWindow time.Duration
}

// New wires intake with the same retry policy used when Deliveries are loaded.
func New(store Store, backoff delivery.BackoffPolicy, retryWindow time.Duration) *Service {
	return &Service{store: store, backoff: backoff, retryWindow: retryWindow}
}

// Request states a Batch in domain values. HTML being non-nil selects inline
// HTML, including an empty body; otherwise TemplateID names an existing Template.
type Request struct {
	Sender        batch.Sender
	Subject       string
	HTML          *string
	TemplateID    string
	GlobalFields  map[string]string
	ScheduledTime *time.Time
	Recipients    []Recipient
	Attachments   batch.Attachments
	Headers       batch.Headers
	Tracking      tracking.Policy
	// TrackingError carries an unreadable Batch Policy from the adapter.
	TrackingError       error
	OneClickUnsubscribe batch.OneClickUnsubscribe
}

// Result is the accepted Batch and its ordered Recipient decisions.
type Result struct {
	Batch         *batch.Batch
	ScheduledTime time.Time
	AcceptedCount int
	Rejected      []RejectedRecipient
}

// RejectedRecipient is input refused without creating a Delivery.
type RejectedRecipient struct {
	Email  string
	Reason RejectionReason
}

// InvalidRequest distinguishes request faults from persistence failures.
type InvalidRequest struct{ Err error }

func (e *InvalidRequest) Error() string { return e.Err.Error() }
func (e *InvalidRequest) Unwrap() error { return e.Err }
func invalid(err error) error           { return &InvalidRequest{Err: err} }

// Accept authorizes before touching storage, decides Recipients independently,
// freezes their Tracking Policy and persists the entire acceptance atomically.
func (s *Service) Accept(ctx context.Context, domain *domains.Domain, p Request) (*Result, error) {
	if strings.ContainsAny(p.Subject, "\r\n") {
		return nil, invalid(errors.New("subject contains forbidden CR/LF"))
	}
	if strings.ContainsAny(p.Sender.Alias, "\r\n") {
		return nil, invalid(errors.New("sender alias contains forbidden CR/LF"))
	}
	host, err := smtputils.GetEmailDomain(p.Sender.Email)
	if err != nil {
		return nil, invalid(fmt.Errorf("invalid sender email %q: %w", p.Sender.Email, err))
	}
	return authz.Guard(ctx, authz.Create, senderBatches(canonicalSenderHost(host), domain.Name()), func() (*Result, error) {
		return s.accept(ctx, domain, p)
	})
}

func (s *Service) accept(ctx context.Context, domain *domains.Domain, p Request) (*Result, error) {
	var tpl, generated *templates.Template
	var err error
	if p.HTML != nil {
		tpl, err = templates.NewTransient(domain.Name(), utils.ReplaceCustomFields(*p.HTML, p.GlobalFields))
		generated = tpl
	} else {
		tpl, err = s.store.FindByDomain(ctx, domain.Name(), p.TemplateID)
		if err != nil {
			return nil, fmt.Errorf("cannot find template with id: %v", p.TemplateID)
		}
		html := utils.ReplaceCustomFields(tpl.Html(), p.GlobalFields)
		if html != tpl.Html() {
			tpl, err = templates.NewTransient(domain.Name(), html)
			generated = tpl
		}
	}
	if err != nil {
		return nil, fmt.Errorf("cannot create template %w", err)
	}
	for i, emails := range [][]string{p.Headers.To, p.Headers.Cc} {
		for _, email := range emails {
			if !smtputils.Validate(email) {
				field := "To"
				if i == 1 {
					field = "Cc"
				}
				return nil, invalid(fmt.Errorf("invalid %s header: %q is not a valid email address", field, email))
			}
		}
	}
	if p.TrackingError != nil {
		return nil, invalid(p.TrackingError)
	}
	if violations := tracking.CeilingViolations(domain.TrackingPolicy(), p.Tracking); len(violations) > 0 {
		return nil, invalid(fmt.Errorf("batch tracking policy exceeds domain ceiling: %s", ceilingViolationDetail("batch", violations)))
	}
	b, err := batch.New(batch.NewParams{
		Domain: domain.Domain(), Subject: p.Subject, Sender: p.Sender,
		TemplateID: tpl.TemplateID(), Attachments: p.Attachments, Headers: p.Headers,
		OneClickUnsubscribe: p.OneClickUnsubscribe, Tracking: p.Tracking,
	})
	if err != nil {
		return nil, invalid(err)
	}
	scheduled := time.Now()
	if p.ScheduledTime != nil {
		scheduled = *p.ScheduledTime
	}
	taken := s.decide(domain, b, p.Recipients, scheduled)
	if err := s.store.Accept(ctx, generated, b, taken.deliveries); err != nil {
		return nil, fmt.Errorf("cannot accept batch %s: %w", b.ID(), err)
	}
	return &Result{Batch: b, ScheduledTime: scheduled, AcceptedCount: len(taken.deliveries), Rejected: taken.rejected}, nil
}

// RejectionReason is why one Recipient was refused at intake, in the stable form a
// caller branches on. Its own type so it cannot be swapped with the free-form detail
// beside it. The values are API contract: see SendRes.rejected_recipients on the wire.
type RejectionReason string

const (
	// ReasonInvalidEmail covers every address Kannon cannot deliver to: empty,
	// whitespace-only, or otherwise refused when the Delivery is built.
	ReasonInvalidEmail RejectionReason = "invalid_email"
	// ReasonTrackingAboveCeiling is a Recipient whose Tracking Policy asks for
	// more than its Domain allows (ADR 0003).
	ReasonTrackingAboveCeiling RejectionReason = "tracking_above_ceiling"
	// ReasonUnsupportedTrackingMode is a Recipient stating a Tracking Mode this build
	// will not act on — in practice a wire value from a newer schema. The caller's work
	// is to restate the Policy in terms this build understands.
	ReasonUnsupportedTrackingMode RejectionReason = "unsupported_tracking_mode"
	// ReasonUnsubscribeURLUnresolved is a Recipient whose fields leave a placeholder in
	// the Batch's one-click unsubscribe URL. Refusing beats sending a DKIM-signed header
	// advertising an authenticated endpoint that is a URL with braces in it (ADR 0005).
	ReasonUnsubscribeURLUnresolved RejectionReason = "unsubscribe_url_unresolved"
)

// acceptance is what became of a Batch's Recipients: those accepted onto the Pool, and
// those Rejected with the reason the caller is told. It exists so that a refusal has
// somewhere to go other than the slog.Warn that dropped it before #364.
type acceptance struct {
	batchID    string
	deliveries []*delivery.Delivery
	rejected   []RejectedRecipient
}

// reject records one Rejected Recipient (CONTEXT.md): no Delivery is created for it.
// reason is the stable token returned; detail is logged for an operator only, as it may
// name internals. The logged address is obfuscated: a caller drives this log's volume.
func (in *acceptance) reject(email string, reason RejectionReason, detail string) {
	slog.Warn("rejecting recipient at intake",
		"batch", in.batchID, "email", utils.ObfuscateEmail(email), "reason", reason, "detail", detail)
	in.rejected = append(in.rejected, RejectedRecipient{Email: email, Reason: reason})
}

// Recipient is one Recipient as a request stated it: the domain value intake
// works in, and what came of translating the Tracking Policy that arrived with it.
//
// The pairing is the whole point. Translating a Recipient can fail — a Mode from a
// newer schema — and that failure Rejects that Recipient alone while the rest of the
// Batch proceeds (#419). Were the translation of the list allowed to return an error,
// one unreadable row would fail the call for thousands of good ones, so the error is
// carried on the row that earned it instead of raised over all of them.
type Recipient struct {
	batch.Recipient
	// TrackingError is what the adapter made of the wire Policy, nil when it
	// translated. It is answered where the cascade is resolved and nowhere earlier, so
	// that the reasons keep the precedence the checks give them: a row with no address
	// and an unreadable Mode is refused for the address, as it always has been.
	TrackingError error
}

func (s *Service) decide(domain *domains.Domain, b *batch.Batch, recipients []Recipient, scheduled time.Time) *acceptance {
	taken := &acceptance{
		batchID:    b.ID().String(),
		deliveries: make([]*delivery.Delivery, 0, len(recipients)),
	}
	for _, r := range recipients {
		if !r.HasAddress() {
			taken.reject(r.Email, ReasonInvalidEmail, "email is empty")
			continue
		}
		policy, rejection := resolveRecipientTracking(domain.TrackingPolicy(), b.TrackingPolicy(), r)
		if rejection != nil {
			taken.reject(r.Email, rejection.reason, rejection.detail)
			continue
		}
		if detail, ok := unresolvedUnsubscribeURL(b.OneClickUnsubscribe(), r.Recipient); !ok {
			taken.reject(r.Email, ReasonUnsubscribeURLUnresolved, detail)
			continue
		}
		d, err := delivery.New(delivery.NewParams{
			BatchID:       b.ID(),
			Email:         r.Email,
			Fields:        r.Fields,
			Domain:        b.Domain(),
			ScheduledTime: scheduled,
			Backoff:       s.backoff,
			RetryWindow:   s.retryWindow,
			Tracking:      policy,
		})
		if err != nil {
			taken.reject(r.Email, ReasonInvalidEmail, err.Error())
			continue
		}
		taken.deliveries = append(taken.deliveries, d)
	}
	return taken
}

// unresolvedUnsubscribeURL reports whether one Recipient's fields can resolve the
// Batch's unsubscribe URL, with an operator-facing detail when they cannot. It reads
// utils.EffectiveFields, the same map the Builder renders with, so the two agree.
func unresolvedUnsubscribeURL(u batch.OneClickUnsubscribe, r batch.Recipient) (string, bool) {
	if u.IsZero() {
		return "", true
	}
	resolved := utils.ReplaceCustomFieldsInURL(u.URLTemplate, utils.EffectiveFields(r.Email, r.Fields))
	if utils.HasUnresolvedPlaceholders(resolved) {
		return fmt.Sprintf("unsubscribe URL still holds a placeholder after substitution: %q", resolved), false
	}
	return "", true
}

// recipientRejection is why one Recipient was refused, split into the stable
// reason the caller branches on and the detail only an operator needs.
type recipientRejection struct {
	reason RejectionReason
	detail string
}

// resolveRecipientTracking collapses the Tracking Policy cascade for one Recipient into
// the Policy frozen on its Delivery (ADR 0003). A Recipient may narrow what the Domain
// and Batch allow; above the Domain's ceiling only that Recipient is Rejected (#419).
//
// A Policy this build could not read is Rejected here rather than at translation, which
// is what keeps the two grounds in one place: whether a Recipient's Policy is admissible
// is one question, whether the Mode is legible only the first way of failing it.
func resolveRecipientTracking(domainPolicy, batchPolicy tracking.Policy, r Recipient) (tracking.Policy, *recipientRejection) {
	if r.TrackingError != nil {
		return tracking.Policy{}, &recipientRejection{
			reason: ReasonUnsupportedTrackingMode,
			detail: r.TrackingError.Error(),
		}
	}
	if violations := tracking.CeilingViolations(domainPolicy, r.Tracking); len(violations) > 0 {
		return tracking.Policy{}, &recipientRejection{
			reason: ReasonTrackingAboveCeiling,
			detail: ceilingViolationDetail("recipient", violations),
		}
	}
	return tracking.Resolve(domainPolicy, batchPolicy, r.Tracking), nil
}

// canonicalSenderHost canonicalises a From host, or returns the zero Name when it is not
// canonical. The trailing dot is trimmed here at the edge, never in the authorization
// layer, which must not normalise (ADR 0008); sender_domain_test.go holds the old rule.
func canonicalSenderHost(host string) values.DomainName {
	f, err := values.Parse(strings.TrimSuffix(strings.TrimSpace(host), "."))
	if err != nil {
		return values.DomainName{}
	}
	return f
}

// senderBatches names the Resource a send needs Create on: the From host's own Batches,
// reachable by a key's sender Grant only when the host is its own Domain. The parent-domain
// allowance, inexpressible by prefix domination, is authorized against the caller's own (ADR 0008).
func senderBatches(from, tenant values.DomainName) authz.Resource {
	if isParentDomain(from, tenant) {
		return authz.Batches(tenant)
	}
	return authz.Batches(from)
}

// isParentDomain reports whether from is a proper parent of tenant, in the sense DNS gives
// the word. Proper, so equality is left to the guard and the two clauses cannot overlap.
// Both arguments are canonical, so this compares and never normalises.
func isParentDomain(from, tenant values.DomainName) bool {
	if from.IsZero() || tenant.IsZero() {
		return false
	}
	return strings.HasSuffix(tenant.String(), "."+from.String())
}

func ceilingViolationDetail(level string, violations []tracking.CeilingViolation) string {
	reasons := make([]string, 0, len(violations))
	for _, v := range violations {
		reasons = append(reasons, fmt.Sprintf(
			"%s: %s asked for %q, which exceeds the domain ceiling %q", v.Axis, level, v.Stated, v.Ceiling))
	}
	return strings.Join(reasons, "; ")
}
