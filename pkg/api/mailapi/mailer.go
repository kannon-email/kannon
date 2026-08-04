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

	// Sending is create on a Domain's Batches (ADR 0008): the guard asks for that
	// authority here, once, in place of the explicit From-domain/tenant comparison that
	// used to stand on this line and could disagree with it.
	res, err := authz.Guard(ctx, authz.Create, senderBatches(from.canonical, domain.Name()),
		func() (*connect.Response[pb.SendRes], error) {
			return s.createBatch(ctx, domain, template, req)
		})
	if err != nil {
		return nil, sendError(err, from.host, domain.Domain())
	}
	return res, nil
}

// createBatch is the send itself, performed only once the caller has been authorized.
// Its own function so the guard wraps the whole of it: a refused send cannot have
// created a Batch row or scheduled a Delivery on its way to being refused.
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
		// Reported even when nothing was refused, so a caller can always reconcile
		// what it submitted against what was queued (#364) — a send in which every
		// Recipient was refused says so instead of looking like an empty success.
		AcceptedCount:      int32(len(taken.deliveries)),
		RejectedCount:      int32(len(taken.rejected)),
		RejectedRecipients: taken.rejected,
	}), nil
}

func (s mailAPIService) Close() error {
	return nil
}

// rejectionReason is why one Recipient was refused at intake, in the stable form a
// caller branches on. Its own type so it cannot be swapped with the free-form detail
// beside it. The values are API contract: see SendRes.rejected_recipients in .proto.
type rejectionReason string

const (
	// reasonInvalidEmail covers every address Kannon cannot deliver to: empty,
	// whitespace-only, or otherwise refused when the Delivery is built.
	reasonInvalidEmail rejectionReason = "invalid_email"
	// reasonTrackingAboveCeiling is a Recipient whose Tracking Policy asks for
	// more than its Domain allows (ADR 0003).
	reasonTrackingAboveCeiling rejectionReason = "tracking_above_ceiling"
	// reasonUnsupportedTrackingMode is a Recipient stating a Tracking Mode this build
	// will not act on — in practice a wire value from a newer schema. The caller's work
	// is to restate the Policy in terms this build understands.
	reasonUnsupportedTrackingMode rejectionReason = "unsupported_tracking_mode"
	// reasonUnsubscribeURLUnresolved is a Recipient whose fields leave a placeholder in
	// the Batch's one-click unsubscribe URL. Refusing beats sending a DKIM-signed header
	// advertising an authenticated endpoint that is a URL with braces in it (ADR 0005).
	reasonUnsubscribeURLUnresolved rejectionReason = "unsubscribe_url_unresolved"
)

// intake is what became of a Batch's Recipients: those accepted onto the Pool, and
// those Rejected with the reason the caller is told. It exists so that a refusal has
// somewhere to go other than the slog.Warn that dropped it before #364.
type intake struct {
	batchID    string
	deliveries []*delivery.Delivery
	rejected   []*pb.RejectedRecipient
}

// reject records one Rejected Recipient (CONTEXT.md): no Delivery is created for it.
// reason is the stable token returned; detail is logged for an operator only, as it may
// name internals. The logged address is obfuscated: a caller drives this log's volume.
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

// unresolvedUnsubscribeURL reports whether one Recipient's fields can resolve the
// Batch's unsubscribe URL, with an operator-facing detail when they cannot. It reads
// utils.EffectiveFields, the same map the Builder renders with, so the two agree.
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

// resolveRecipientTracking collapses the Tracking Policy cascade for one Recipient into
// the Policy frozen on its Delivery (ADR 0003). A Recipient may narrow what the Domain
// and Batch allow; above the Domain's ceiling only that Recipient is Rejected (#419).
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

// authenticate resolves the HTTP Basic credential (<domain>:<key>) into its Domain and a
// context carrying that key's Principal — the context, so that dropping it fails closed.
// Every refusal is the same error, so nothing about which Domains or keys exist leaks.
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

	// The credential is the other place a domain name enters from the wire, so it is
	// canonicalised here. A username that is not a domain name gets the same generic
	// error as a bad key, so nothing about which domains exist leaks.
	callDomain, err := values.Parse(domainName)
	if err != nil {
		return nil, nil, errors.New("invalid auth")
	}

	apiKey, err := s.apiKeys.ValidateForAuth(ctx, callDomain, key)
	if err != nil {
		// Always return generic error (security requirement)
		return nil, nil, errors.New("invalid auth")
	}

	// The key authenticated, so it now says what it may do: sender on its own Domain.
	// A failure here is a corrupt row rather than a bad request, so the request is refused
	// rather than continued with a Principal holding less than the send needs.
	principal, err := apiKey.Principal()
	if err != nil {
		slog.Error("cannot resolve api key to a principal", "err", err)
		return nil, nil, errors.New("invalid auth")
	}

	domain, err := s.domains.FindByName(ctx, apiKey.DomainName())
	if err != nil {
		return nil, nil, errors.New("invalid auth")
	}

	return authz.NewContext(ctx, principal), domain, nil
}

// senderAddress is a Batch's From address as intake resolved it: canonical is what the
// authority model compares (ADR 0008), host is the text a refusal quotes back. A host
// that cannot be canonicalised leaves canonical zero, which no Anchor covers.
type senderAddress struct {
	host      string
	canonical values.DomainName
}

// senderAddressOf validates a Batch's From address and resolves its host. Everything
// checked here is a property of the request — absent Sender, header-injecting Alias, no
// host at all — never of the caller's authority, which is the guard's question.
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

// sendError renders whatever the guarded send returned. A refusal keeps the code and the
// wording the old tenant comparison produced, to the letter, so no client can tell the
// mechanism changed (#438); ErrNoPrincipal stays apart, being a different problem.
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

// sendTrackingPolicyError maps an untranslatable wire Tracking Policy onto a Connect code:
// a Mode this build does not know is a bad argument. It answers that sentinel exactly as
// adminapi.trackingPolicyError does, and the pair must change together.
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

// batchTrackingCeilingError builds the error for a Batch above its Domain's ceiling
// (ADR 0003). Unlike a Recipient, which is Rejected alone, a Batch fails the whole call:
// it is one instruction about the whole send, with no part of it left to honour.
func batchTrackingCeilingError(violations []tracking.CeilingViolation) error {
	return connect.NewError(connect.CodeInvalidArgument,
		fmt.Errorf("batch tracking policy exceeds domain ceiling: %s",
			ceilingViolationDetail("batch", violations)))
}

// ceilingViolationDetail names every offending axis, what the ceiling allows and what was
// asked for. Silent clamping would leave the ceiling indistinguishable from a bug
// (ADR 0003), so both levels say exactly what happened.
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
