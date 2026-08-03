package mailapi

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"connectrpc.com/connect"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/kannon-email/kannon/internal/apikeys"
	"github.com/kannon-email/kannon/internal/authz"
	"github.com/kannon-email/kannon/internal/authzconnect"
	"github.com/kannon-email/kannon/internal/batch"
	sqlc "github.com/kannon-email/kannon/internal/db"
	"github.com/kannon-email/kannon/internal/delivery"
	"github.com/kannon-email/kannon/internal/domains"
	smtputils "github.com/kannon-email/kannon/internal/smtp"
	"github.com/kannon-email/kannon/internal/templates"
	"github.com/kannon-email/kannon/internal/tracking"
	"github.com/kannon-email/kannon/internal/trackingpb"
	"github.com/kannon-email/kannon/internal/utils"
	"github.com/kannon-email/kannon/internal/values"
	pb "github.com/kannon-email/kannon/proto/kannon/mailer/apiv1"
	mailerv1connect "github.com/kannon-email/kannon/proto/kannon/mailer/apiv1/apiv1connect"
	mailertypes "github.com/kannon-email/kannon/proto/kannon/mailer/types"
	trackingtypes "github.com/kannon-email/kannon/proto/kannon/tracking/types"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type mailAPIService struct {
	domains    domains.Repository
	apiKeys    *apikeys.Service
	templates  templates.Repository
	batches    batch.Repository
	deliveries delivery.Repository
	backoff    delivery.BackoffPolicy
	// retryWindow is the Retry Budget stamped on every Delivery created at
	// intake. It travels with backoff so a fresh Delivery cannot carry a
	// different budget from the same Delivery once rehydrated from its row.
	retryWindow time.Duration
}

func (s mailAPIService) SendHTML(ctx context.Context, req *connect.Request[pb.SendHTMLReq]) (*connect.Response[pb.SendRes], error) {
	ctx, domain, err := s.authenticate(ctx, req.Header())
	if err != nil {
		return nil, errors.New("invalid or wrong auth")
	}

	req.Msg.Html = utils.ReplaceCustomFields(req.Msg.Html, req.Msg.GlobalFields)

	template, err := s.createTransientTemplate(ctx, domain.Name(), req.Msg.Html)
	if err != nil {
		slog.Error("cannot create template", "err", err)
		return nil, fmt.Errorf("cannot create template %w", err)
	}

	res := &pb.SendTemplateReq{
		Sender:              req.Msg.Sender,
		Subject:             req.Msg.Subject,
		TemplateId:          template.TemplateID(),
		ScheduledTime:       req.Msg.ScheduledTime,
		Recipients:          req.Msg.Recipients,
		Attachments:         req.Msg.Attachments,
		GlobalFields:        nil,
		Headers:             req.Msg.Headers,
		Tracking:            req.Msg.Tracking,
		OneClickUnsubscribe: req.Msg.OneClickUnsubscribe,
	}

	return s.sendTemplate(ctx, domain, connect.NewRequest(res))
}

func (s mailAPIService) SendTemplate(ctx context.Context, req *connect.Request[pb.SendTemplateReq]) (*connect.Response[pb.SendRes], error) {
	ctx, domain, err := s.authenticate(ctx, req.Header())
	if err != nil {
		return nil, errors.New("invalid or wrong auth")
	}

	return s.sendTemplate(ctx, domain, req)
}

func (s mailAPIService) sendTemplate(ctx context.Context, domain *domains.Domain, req *connect.Request[pb.SendTemplateReq]) (*connect.Response[pb.SendRes], error) {
	if err := assertHeaderSafe("subject", req.Msg.Subject); err != nil {
		return nil, err
	}

	template, err := s.templates.FindByDomain(ctx, domain.Name(), req.Msg.TemplateId)
	if err != nil {
		slog.Error("cannot find template", "err", err)
		return nil, fmt.Errorf("cannot find template with id: %v", req.Msg.TemplateId)
	}

	template, err = s.createTemplateWithGlobalFields(ctx, template, req.Msg.GlobalFields)
	if err != nil {
		slog.Error("cannot create transient template", "err", err)
		return nil, fmt.Errorf("cannot create template %w", err)
	}

	from, err := senderAddressOf(req.Msg.Sender)
	if err != nil {
		return nil, err
	}

	// Sending is create on a Domain's Batches (ADR 0008), so the authority a send
	// needs is asked for here, once, by the guard — the explicit comparison of the
	// From domain against the authenticated tenant that used to stand on this line
	// is gone, and with it the possibility of the two mechanisms disagreeing.
	res, err := authz.Guard(ctx, authz.Create, senderBatches(from.canonical, domain.Name()),
		func() (*connect.Response[pb.SendRes], error) {
			return s.createBatch(ctx, domain, template, req)
		})
	if err != nil {
		return nil, sendError(err, from.host, domain.Domain())
	}
	return res, nil
}

// createBatch is the send itself, performed only once the caller has been
// authorized to make it.
//
// It is a function of its own so that the guard wraps the whole of it: "check,
// then proceed" is then one expression with nothing to fall through, and a refused
// send cannot have created a Batch row or scheduled a Delivery on its way to being
// refused.
func (s mailAPIService) createBatch(ctx context.Context, domain *domains.Domain, template *templates.Template, req *connect.Request[pb.SendTemplateReq]) (*connect.Response[pb.SendRes], error) {
	sender := batch.Sender{
		Email: req.Msg.Sender.Email,
		Alias: req.Msg.Sender.Alias,
	}

	scheduled := time.Now()
	if req.Msg.ScheduledTime != nil {
		scheduled = req.Msg.ScheduledTime.AsTime()
	}

	attachments := make(batch.Attachments, len(req.Msg.Attachments))
	for _, r := range req.Msg.Attachments {
		attachments[r.Filename] = r.Content
	}

	customHeaders, err := validateHeaders(req.Msg.Headers)
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}

	batchPolicy, err := trackingpb.ToPolicy(req.Msg.Tracking)
	if err != nil {
		return nil, sendTrackingPolicyError(err)
	}
	// The Batch may only restrict what the Domain allows, never widen it
	// (ADR 0003). Checked here, at intake, so the caller can tell a policy
	// decision from a bug rather than having it silently clamped.
	if violations := tracking.CeilingViolations(domain.TrackingPolicy(), batchPolicy); len(violations) > 0 {
		return nil, batchTrackingCeilingError(violations)
	}

	// A malformed or non-https endpoint is a fault in the request as a whole,
	// not in one of its rows, so it fails the call rather than refusing
	// Recipients one by one (ADR 0005). batch.New holds the invariant.
	b, err := batch.New(batch.NewParams{
		Domain:              domain.Domain(),
		Subject:             req.Msg.Subject,
		Sender:              sender,
		TemplateID:          template.TemplateID(),
		Attachments:         attachments,
		Headers:             customHeaders,
		OneClickUnsubscribe: unsubscribeFromRequest(req.Msg.OneClickUnsubscribe),
		Tracking:            batchPolicy,
	})
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}

	taken, err := s.scheduleBatch(ctx, domain, b, req.Msg.Recipients, scheduled)
	if err != nil {
		slog.Error("cannot create pool", "err", err)
		return nil, err
	}

	return connect.NewResponse(&pb.SendRes{
		MessageId:     b.ID().String(),
		TemplateId:    template.TemplateID(),
		ScheduledTime: timestamppb.New(scheduled),
		// Reported even when nothing was refused, so a caller can always
		// reconcile what it submitted against what was queued (#364). A send in
		// which every Recipient was refused reports that here rather than
		// looking like a success with an empty Pool.
		AcceptedCount:      int32(len(taken.deliveries)),
		RejectedCount:      int32(len(taken.rejected)),
		RejectedRecipients: taken.rejected,
	}), nil
}

func (s mailAPIService) Close() error {
	return nil
}

// rejectionReason is why one Recipient was refused at intake, in the stable
// machine-readable form a caller branches on. It has its own type so it cannot be
// swapped with the operator-facing detail beside it, which is free-form and never
// returned.
//
// The values are part of the API contract, documented on
// SendRes.rejected_recipients in .proto/kannon/mailer/apiv1/mailerapiv1.proto;
// they must not be reworded here without changing that.
type rejectionReason string

const (
	// reasonInvalidEmail covers every address Kannon cannot deliver to: empty,
	// whitespace-only, or otherwise refused when the Delivery is built.
	reasonInvalidEmail rejectionReason = "invalid_email"
	// reasonTrackingAboveCeiling is a Recipient whose Tracking Policy asks for
	// more than its Domain allows (ADR 0003).
	reasonTrackingAboveCeiling rejectionReason = "tracking_above_ceiling"
	// reasonUnsupportedTrackingMode is a Recipient stating a Tracking Mode this
	// build will not act on — in practice a wire value from a newer schema, since
	// every Mode this build knows is one it honours. The work it leaves the
	// caller is to restate the Policy in terms this build understands.
	reasonUnsupportedTrackingMode rejectionReason = "unsupported_tracking_mode"
	// reasonUnsubscribeURLUnresolved is a Recipient whose fields leave a
	// placeholder in the Batch's one-click unsubscribe URL unresolved. Refusing
	// it beats sending it: the alternative is a DKIM-signed header advertising an
	// authenticated one-click endpoint that is in fact a URL with braces in it,
	// and a recipient who presses the button and stays subscribed (ADR 0005).
	reasonUnsubscribeURLUnresolved rejectionReason = "unsubscribe_url_unresolved"
)

// intake is what became of a Batch's Recipients as they were taken in: those
// accepted onto the Pool, and those Rejected with the reason the caller is told.
//
// It exists so that a refusal has somewhere to go other than a log line. Before
// #364 every rejection was dropped with a slog.Warn and the call reported
// success regardless, so a caller submitting a thousand bad rows got a Batch id
// and no way to discover that nothing had been queued.
type intake struct {
	batchID    string
	deliveries []*delivery.Delivery
	rejected   []*pb.RejectedRecipient
}

// reject records one Rejected Recipient (CONTEXT.md): no Delivery is created for
// it and none will be attempted. reason is the stable token returned to the
// caller; detail is logged for an operator and deliberately not returned, since
// it may name internals.
//
// The address is obfuscated in the log, as everywhere else in the codebase, but
// returned to the caller in full: a caller may reconcile against its own input,
// while a log a caller can drive the volume of — one line per submitted Recipient
// — must not become a route for recipient addresses into log aggregators. That
// would be a poor trade in a feature whose purpose is to retain less.
func (in *intake) reject(email string, reason rejectionReason, detail string) {
	slog.Warn("rejecting recipient at intake",
		"batch", in.batchID, "email", utils.ObfuscateEmail(email), "reason", reason, "detail", detail)
	in.rejected = append(in.rejected, &pb.RejectedRecipient{Email: email, Reason: string(reason)})
}

func (s mailAPIService) scheduleBatch(ctx context.Context, domain *domains.Domain, b *batch.Batch, recipients []*mailertypes.Recipient, scheduled time.Time) (*intake, error) {
	if err := s.batches.Create(ctx, b); err != nil {
		return nil, err
	}
	taken := &intake{
		batchID:    b.ID().String(),
		deliveries: make([]*delivery.Delivery, 0, len(recipients)),
	}
	for _, r := range recipients {
		if strings.TrimSpace(r.Email) == "" {
			taken.reject(r.Email, reasonInvalidEmail, "email is empty")
			continue
		}
		policy, rejection := resolveRecipientTracking(domain.TrackingPolicy(), b.TrackingPolicy(), r.Tracking)
		if rejection != nil {
			taken.reject(r.Email, rejection.reason, rejection.detail)
			continue
		}
		if detail, ok := unresolvedUnsubscribeURL(b.OneClickUnsubscribe(), r.Email, r.Fields); !ok {
			taken.reject(r.Email, reasonUnsubscribeURLUnresolved, detail)
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
			taken.reject(r.Email, reasonInvalidEmail, err.Error())
			continue
		}
		taken.deliveries = append(taken.deliveries, d)
	}
	if len(taken.deliveries) == 0 {
		return taken, nil
	}
	if err := s.deliveries.Schedule(ctx, taken.deliveries...); err != nil {
		return nil, fmt.Errorf("cannot schedule deliveries for batch %s: %w", b.ID().String(), err)
	}
	return taken, nil
}

// unsubscribeFromRequest maps the wire type onto the domain value object. A
// caller stating nothing yields the zero value, and no unsubscribe header.
func unsubscribeFromRequest(u *mailertypes.OneClickUnsubscribe) batch.OneClickUnsubscribe {
	if u == nil {
		return batch.OneClickUnsubscribe{}
	}
	return batch.OneClickUnsubscribe{URLTemplate: u.UrlTemplate}
}

// unresolvedUnsubscribeURL reports whether one Recipient's fields can resolve
// the Batch's unsubscribe URL, returning an operator-facing detail when they
// cannot.
//
// The check runs against utils.EffectiveFields, the same map the Builder will
// render with, so that a Recipient accepted here is one the Builder can resolve.
// Anything else would either queue a Delivery that silently loses its
// unsubscribe header, or refuse a Recipient that would have been fine.
func unresolvedUnsubscribeURL(u batch.OneClickUnsubscribe, email string, fields map[string]string) (string, bool) {
	if u.IsZero() {
		return "", true
	}
	resolved := utils.ReplaceCustomFieldsInURL(u.URLTemplate, utils.EffectiveFields(email, fields))
	if utils.HasUnresolvedPlaceholders(resolved) {
		return fmt.Sprintf("unsubscribe URL still holds a placeholder after substitution: %q", resolved), false
	}
	return "", true
}

// recipientRejection is why one Recipient was refused, split into the stable
// reason the caller branches on and the detail only an operator needs.
type recipientRejection struct {
	reason rejectionReason
	detail string
}

// resolveRecipientTracking collapses the Tracking Policy cascade for one
// Recipient into the concrete Policy that will be frozen on its Delivery
// (ADR 0003), or reports why that Recipient is Rejected instead.
//
// The Recipient is the most specific of the three levels, so its statement can
// narrow whatever the Domain and the Batch allow — a Recipient-level `off` wins
// over a Domain-level `full`, because consent must always be honourable.
//
// A Recipient's ceiling is the *Domain's* Policy, not the Batch's: a Recipient
// below its Batch is ordinary resolution, while a Recipient above its Domain
// asks to collect more than the operator authorised, which consent cannot buy.
// That is refused — but only that Recipient, so one bad row does not fail a send
// of thousands (#419). This is the same comparison the Batch level makes in
// sendTemplate; only the consequence differs.
func resolveRecipientTracking(domainPolicy, batchPolicy tracking.Policy, stated *trackingtypes.TrackingPolicy) (tracking.Policy, *recipientRejection) {
	recipientPolicy, err := trackingpb.ToPolicy(stated)
	if err != nil {
		return tracking.Policy{}, &recipientRejection{
			reason: reasonUnsupportedTrackingMode,
			detail: err.Error(),
		}
	}
	if violations := tracking.CeilingViolations(domainPolicy, recipientPolicy); len(violations) > 0 {
		return tracking.Policy{}, &recipientRejection{
			reason: reasonTrackingAboveCeiling,
			detail: ceilingViolationDetail("recipient", violations),
		}
	}
	return tracking.Resolve(domainPolicy, batchPolicy, recipientPolicy), nil
}

func (s mailAPIService) createTemplateWithGlobalFields(ctx context.Context, template *templates.Template, globalFields map[string]string) (*templates.Template, error) {
	if len(globalFields) == 0 {
		return template, nil
	}

	newHTML := utils.ReplaceCustomFields(template.Html(), globalFields)
	if newHTML == template.Html() {
		return template, nil
	}

	return s.createTransientTemplate(ctx, template.DomainName(), newHTML)
}

func (s mailAPIService) createTransientTemplate(ctx context.Context, domain values.DomainName, html string) (*templates.Template, error) {
	tpl, err := templates.NewTransient(domain, html)
	if err != nil {
		return nil, err
	}
	if err := s.templates.Create(ctx, tpl); err != nil {
		return nil, err
	}
	return tpl, nil
}

// authenticate resolves the credential on an incoming request into the Domain it
// belongs to and a context carrying that credential's Principal.
//
// HTTP Basic carrying <fqdn>:<key>. This is the one place in this package that
// knows a credential was verified, so it is where the Principal is planted; every
// guard downstream reads it from the context and nothing else in this package puts
// one there.
//
// It returns the context rather than the Principal so that forgetting to use it
// fails closed: a handler that dropped it would reach the guard with nothing
// authenticating the request and every send would be refused, instead of one being
// performed under an authority nobody resolved.
//
// Every refusal is the same error whatever the cause — a username that is not an
// FQDN, an unknown, expired or deactivated key, a Domain deleted since the key was
// minted — so that nothing about which Domains or keys exist leaks.
func (s mailAPIService) authenticate(ctx context.Context, headers http.Header) (context.Context, *domains.Domain, error) {
	auth := headers.Get("Authorization")

	if !strings.HasPrefix(auth, "Basic ") {
		return nil, nil, errors.New("invalid auth")
	}

	token := strings.Replace(auth, "Basic ", "", 1)
	data, err := base64.StdEncoding.DecodeString(token)
	if err != nil {
		return nil, nil, errors.New("invalid auth")
	}

	authData := string(data)

	parts := strings.Split(authData, ":")
	if len(parts) != 2 {
		return nil, nil, errors.New("invalid auth")
	}
	domainName, key := parts[0], parts[1]

	// The credential is the other place an FQDN enters from the wire, so it is
	// canonicalised here. A username that is not an FQDN at all cannot match a
	// Domain, and is answered with the same generic error as a bad key so that
	// nothing about which domains exist leaks.
	callDomain, err := values.Parse(domainName)
	if err != nil {
		return nil, nil, errors.New("invalid auth")
	}

	// Use API key repository for authentication
	apiKey, err := s.apiKeys.ValidateForAuth(ctx, callDomain, key)
	if err != nil {
		// Always return generic error (security requirement)
		return nil, nil, errors.New("invalid auth")
	}

	// The key authenticated, so it now says what it may do: sender on its own
	// Domain and nothing else. A failure here is a Domain that cannot carry an
	// Anchor — a corrupt row, not a bad request — and the request is refused
	// rather than continued with a Principal holding less, which would be turned
	// away by the guard below with a message blaming the caller's From address.
	principal, err := apiKey.Principal()
	if err != nil {
		slog.Error("cannot resolve api key to a principal", "err", err)
		return nil, nil, errors.New("invalid auth")
	}

	// Fetch full domain info
	domain, err := s.domains.FindByName(ctx, apiKey.DomainName())
	if err != nil {
		return nil, nil, errors.New("invalid auth")
	}

	return authz.NewContext(ctx, principal), domain, nil
}

// senderAddress is a Batch's From address as intake resolved it.
//
// Two forms of one host, kept together because both are needed and neither will do
// for the other's job. canonical is what the authority model compares, since a
// Resource segment must have exactly one spelling (ADR 0008); host is the text the
// caller wrote, which is what a refusal quotes back, so that the message a client
// has always received does not silently change spelling.
//
// A host that cannot be a canonical FQDN leaves canonical zero, and that is not an
// error state for a caller to branch on: the zero FQDN composes a Resource with an
// empty segment, which nothing covers, so the guard refuses it by the same
// mechanism and with the same message as a host belonging to somebody else. Which
// is the right answer — a host no Domain can be spelled as is neither the caller's
// own Domain nor a parent of it.
type senderAddress struct {
	host      string
	canonical values.DomainName
}

// senderAddressOf validates a Batch's From address and resolves its host.
//
// Everything checked here is a property of the request rather than of the caller's
// authority: an absent Sender, an Alias that would inject an SMTP header, an
// address with no host to speak of. Whether the caller may send *as* that host is a
// different question with a different answer, and the guard asks it.
func senderAddressOf(s *mailertypes.Sender) (senderAddress, error) {
	if s == nil {
		return senderAddress{}, connect.NewError(connect.CodeInvalidArgument, errors.New("sender is required"))
	}
	if err := assertHeaderSafe("sender alias", s.Alias); err != nil {
		return senderAddress{}, err
	}
	host, err := smtputils.GetEmailDomain(s.Email)
	if err != nil {
		return senderAddress{}, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("invalid sender email %q: %w", s.Email, err))
	}
	return senderAddress{host: host, canonical: canonicalSenderHost(host)}, nil
}

// canonicalSenderHost canonicalises the host of a From address, or returns the zero
// FQDN when it is not one.
//
// The trailing dot is trimmed before values.Parse, which refuses one. That is not a
// liberty taken here: the check this replaces already tolerated it (a TrimSuffix in
// the old senderDomainAllowed), "example.com." and "example.com" are one mail
// domain to DNS, SMTP and DKIM, and narrowing what a customer may send as is as
// much a regression as widening it. Lower-casing is values.Parse's own, for the same
// reason the old code did it.
//
// The trimming happens here, at the edge, and not in the authorization layer, which
// must never normalise anything (ADR 0008): a Grant on "TEST.com" reaching another
// Domain's data would be the escalation rather than the convenience.
//
// One host the old string rule permitted is refused here, and it is unreachable
// rather than accepted: a single label, which was a suffix of the tenant and so a
// permitted parent, is not a canonical FQDN — "batches", "stats" and "apikeys" are
// all valid single-label hostnames and also segments of the Resource tree. No
// address of the form "a@com" gets past smtputils.Validate, which wants a dot after
// the "@", so no send ever asked for one. The differential in sender_domain_test.go
// holds that reasoning to the code: it fails if this or any other difference from
// the old rule becomes reachable.
func canonicalSenderHost(host string) values.DomainName {
	f, err := values.Parse(strings.TrimSuffix(strings.TrimSpace(host), "."))
	if err != nil {
		return values.DomainName{}
	}
	return f
}

// senderBatches names the Resource a send must hold Create on for its From host to
// be permitted.
//
// Normally that is the host's own Batches. Sending is create on a Domain's Batches
// (ADR 0008) and the From host is what says which Domain the mail goes out as, so a
// key — sender anchored on its own Domain, whose one rule composes
// domains/<own>/batches — reaches domains/<host>/batches under prefix domination
// exactly when the host *is* its own Domain. That single check is the whole of the
// cross-Domain refusal an explicit tenant comparison used to perform here, now
// performed by the one matcher that answers every other question about authority in
// the system.
//
// The exception is the parent-domain allowance, which Kannon has always granted and
// which the path model cannot express: a tenant authenticated as k.example.com may
// send from @example.com. Those are two Resources differing in their second
// segment, and prefix domination compares segments while this rule is hostname
// suffix matching, so no Anchor covers both. Two ways to fold it into the model were
// considered and rejected:
//
//   - Give the key a second Grant on the parent Domain. That is real authority over
//     another Domain's Batches — a Domain that may belong to another customer — and
//     every future guard on that path would honour it, not only this one. ADR 0008
//     is explicit that an API Key carries exactly one fixed Grant.
//   - Teach the matcher about hostnames. It is the single matcher in the system, so
//     that would change the meaning of every Grant ever issued, in a layer whose
//     whole discipline is that it compares and never interprets.
//
// So the parent case is authorized against the caller's *own* Batches, which is what
// this returns for it. What the guard then verifies is precisely the authority the
// caller holds, so the allowance widens which host may appear in From and can never
// widen who may send: a Principal that may not send for its own Domain is refused
// here too — which the old comparison, reached only after authentication and
// consulting no authority at all, could not manage.
func senderBatches(from, tenant values.DomainName) authz.Resource {
	if isParentDomain(from, tenant) {
		return authz.Batches(tenant)
	}
	return authz.Batches(from)
}

// isParentDomain reports whether from is a proper parent of tenant, in the sense
// DNS gives the word: the parent of a.b.example.com is b.example.com, and so on up.
// The last label alone is not among them, since it cannot be a canonical FQDN —
// see canonicalSenderHost for why nothing ever asks.
//
// Proper, so equality is left to the guard. The two clauses of the rule must not
// overlap, or the exception would be quietly doing the guard's work and a change to
// the authority model would leave no trace here.
//
// Both arguments are canonical, so this compares and never normalises: the
// lower-casing and trailing-dot trimming the old string version performed have
// already happened at the edge. The zero FQDN is nobody's parent, so a host that
// could not be canonicalised reaches the guard's refusal rather than this
// allowance.
func isParentDomain(from, tenant values.DomainName) bool {
	if from.IsZero() || tenant.IsZero() {
		return false
	}
	return strings.HasSuffix(tenant.String(), "."+from.String())
}

// sendError renders whatever the guarded send returned as the error its caller
// receives.
//
// An authorization refusal keeps the code and the wording the explicit tenant
// comparison produced — permission denied, `sender domain %q is not authorized for
// tenant %q`, the host quoted as the caller wrote it — deliberately and to the
// letter. What refuses a cross-Domain sender has changed; a client branching on the
// code, or matching the message, must not be able to tell.
//
// ErrNoPrincipal is unreachable through either handler, since both authenticate
// first and authentication is what plants the Principal. It is rendered apart from
// the refusal above rather than folded into it because the two are different
// operational problems, and authzconnect.Error is where that difference is logged:
// were a future path ever to reach a send without authenticating, the log should say
// so instead of blaming the caller's From address.
//
// Everything else is returned exactly as it arrived, so every other Connect code a
// send can produce is untouched.
func sendError(err error, host, tenant string) error {
	switch {
	case errors.Is(err, authz.ErrForbidden):
		return connect.NewError(connect.CodePermissionDenied,
			fmt.Errorf("sender domain %q is not authorized for tenant %q", host, tenant))
	case errors.Is(err, authz.ErrNoPrincipal):
		return authzconnect.Error(err, connect.CodePermissionDenied)
	default:
		return err
	}
}

// assertHeaderSafe rejects strings containing CR or LF, which would let an
// attacker inject arbitrary SMTP headers when the value is interpolated into
// a header line.
func assertHeaderSafe(field, v string) error {
	if strings.ContainsAny(v, "\r\n") {
		return connect.NewError(connect.CodeInvalidArgument,
			fmt.Errorf("%s contains forbidden CR/LF", field))
	}
	return nil
}

// sendTrackingPolicyError maps a failure to translate a Batch's wire Tracking
// Policy onto a Connect code: a Mode this build does not know is a bad
// argument.
//
// It answers that `trackingpb` sentinel exactly as
// pkg/api/adminapi.trackingPolicyError does, deliberately, so that one bad Mode
// does not mean two different things depending on which API was asked; the two
// are a pair and should change together. It is named apart from that one
// because the rest of its mapping differs — anything else a send can fail on is
// the caller's argument, never a missing Domain.
func sendTrackingPolicyError(err error) error {
	switch {
	case errors.Is(err, trackingpb.ErrUnknownMode):
		return connect.NewError(connect.CodeInvalidArgument, err)
	default:
		// A caller sending a Tracking Policy Kannon cannot make sense of is a
		// bad argument, not a server fault.
		return connect.NewError(connect.CodeInvalidArgument, err)
	}
}

// batchTrackingCeilingError builds the error for a Batch stating a Tracking
// Mode above its Domain's ceiling (ADR 0003). Unlike a Recipient, which is
// Rejected on its own, a Batch above the ceiling fails the whole call: it is
// one instruction about the whole send, and there is no part of it to honour.
func batchTrackingCeilingError(violations []tracking.CeilingViolation) error {
	return connect.NewError(connect.CodeInvalidArgument,
		fmt.Errorf("batch tracking policy exceeds domain ceiling: %s",
			ceilingViolationDetail("batch", violations)))
}

// ceilingViolationDetail renders ceiling violations as one sentence naming every
// offending axis, what the ceiling allows, and what was asked for. Silent
// clamping would leave the ceiling indistinguishable from a bug (ADR 0003), so
// both levels say exactly what happened — the Batch in the error that fails the
// call, the Recipient in the log accompanying its rejection.
func ceilingViolationDetail(level string, violations []tracking.CeilingViolation) string {
	reasons := make([]string, 0, len(violations))
	for _, v := range violations {
		reasons = append(reasons, fmt.Sprintf(
			"%s: %s asked for %q, which exceeds the domain ceiling %q", v.Axis, level, v.Stated, v.Ceiling))
	}
	return strings.Join(reasons, "; ")
}

func validateHeaders(h *mailertypes.Headers) (batch.Headers, error) {
	if h == nil {
		return batch.Headers{}, nil
	}
	for _, email := range h.To {
		if !smtputils.Validate(email) {
			return batch.Headers{}, fmt.Errorf("invalid To header: %q is not a valid email address", email)
		}
	}
	for _, email := range h.Cc {
		if !smtputils.Validate(email) {
			return batch.Headers{}, fmt.Errorf("invalid Cc header: %q is not a valid email address", email)
		}
	}
	return batch.Headers{To: h.To, Cc: h.Cc}, nil
}

func NewMailerAPIV1(db *pgxpool.Pool, backoff delivery.BackoffPolicy, retryWindow time.Duration) mailerv1connect.MailerHandler {
	domainsCli := sqlc.NewDomainsRepository(db)
	apiKeysRepo := sqlc.NewAPIKeysRepository(db)
	apiKeysService := apikeys.NewService(apiKeysRepo)
	batchRepo := sqlc.NewBatchRepository(db)
	deliveryRepo := sqlc.NewDeliveryRepository(db, backoff, retryWindow)
	templatesRepo := sqlc.NewTemplatesRepository(db)

	return &mailAPIService{
		domains:     domainsCli,
		apiKeys:     apiKeysService,
		batches:     batchRepo,
		deliveries:  deliveryRepo,
		templates:   templatesRepo,
		backoff:     backoff,
		retryWindow: retryWindow,
	}
}
