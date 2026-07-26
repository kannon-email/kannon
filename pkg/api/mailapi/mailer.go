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
	"github.com/kannon-email/kannon/internal/batch"
	sqlc "github.com/kannon-email/kannon/internal/db"
	"github.com/kannon-email/kannon/internal/delivery"
	"github.com/kannon-email/kannon/internal/domains"
	smtputils "github.com/kannon-email/kannon/internal/smtp"
	"github.com/kannon-email/kannon/internal/templates"
	"github.com/kannon-email/kannon/internal/tracking"
	"github.com/kannon-email/kannon/internal/trackingpb"
	"github.com/kannon-email/kannon/internal/utils"
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
}

func (s mailAPIService) SendHTML(ctx context.Context, req *connect.Request[pb.SendHTMLReq]) (*connect.Response[pb.SendRes], error) {
	domain, err := s.getCallDomainFromHeaders(ctx, req.Header())
	if err != nil {
		return nil, errors.New("invalid or wrong auth")
	}

	req.Msg.Html = utils.ReplaceCustomFields(req.Msg.Html, req.Msg.GlobalFields)

	template, err := s.createTransientTemplate(ctx, domain.Domain(), req.Msg.Html)
	if err != nil {
		slog.Error("cannot create template", "err", err)
		return nil, fmt.Errorf("cannot create template %w", err)
	}

	res := &pb.SendTemplateReq{
		Sender:        req.Msg.Sender,
		Subject:       req.Msg.Subject,
		TemplateId:    template.TemplateID(),
		ScheduledTime: req.Msg.ScheduledTime,
		Recipients:    req.Msg.Recipients,
		Attachments:   req.Msg.Attachments,
		GlobalFields:  nil,
		Headers:       req.Msg.Headers,
		Tracking:      req.Msg.Tracking,
	}

	return s.sendTemplate(ctx, domain, connect.NewRequest(res))
}

func (s mailAPIService) SendTemplate(ctx context.Context, req *connect.Request[pb.SendTemplateReq]) (*connect.Response[pb.SendRes], error) {
	domain, err := s.getCallDomainFromHeaders(ctx, req.Header())
	if err != nil {
		return nil, errors.New("invalid or wrong auth")
	}

	return s.sendTemplate(ctx, domain, req)
}

func (s mailAPIService) sendTemplate(ctx context.Context, domain *domains.Domain, req *connect.Request[pb.SendTemplateReq]) (*connect.Response[pb.SendRes], error) {
	if err := assertHeaderSafe("subject", req.Msg.Subject); err != nil {
		return nil, err
	}

	template, err := s.templates.FindByDomain(ctx, domain.Domain(), req.Msg.TemplateId)
	if err != nil {
		slog.Error("cannot find template", "err", err)
		return nil, fmt.Errorf("cannot find template with id: %v", req.Msg.TemplateId)
	}

	template, err = s.createTemplateWithGlobalFields(ctx, template, req.Msg.GlobalFields)
	if err != nil {
		slog.Error("cannot create transient template", "err", err)
		return nil, fmt.Errorf("cannot create template %w", err)
	}

	if err := validateSender(req.Msg.Sender, domain.Domain()); err != nil {
		return nil, err
	}

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

	b, err := batch.New(domain.Domain(), req.Msg.Subject, sender, template.TemplateID(), attachments, customHeaders, batchPolicy)
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
	// build will not act on: the reserved `pseudonymous`, or a wire value from a
	// newer schema. Both leave the caller the same work — restate the Policy —
	// so they share one reason.
	reasonUnsupportedTrackingMode rejectionReason = "unsupported_tracking_mode"
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
		d, err := delivery.New(delivery.NewParams{
			BatchID:       b.ID(),
			Email:         r.Email,
			Fields:        r.Fields,
			Domain:        b.Domain(),
			ScheduledTime: scheduled,
			Backoff:       s.backoff,
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

	return s.createTransientTemplate(ctx, template.Domain(), newHTML)
}

func (s mailAPIService) createTransientTemplate(ctx context.Context, domain, html string) (*templates.Template, error) {
	tpl, err := templates.NewTransient(domain, html)
	if err != nil {
		return nil, err
	}
	if err := s.templates.Create(ctx, tpl); err != nil {
		return nil, err
	}
	return tpl, nil
}

func (s mailAPIService) getCallDomainFromHeaders(ctx context.Context, headers http.Header) (*domains.Domain, error) {
	auth := headers.Get("Authorization")

	if !strings.HasPrefix(auth, "Basic ") {
		return nil, errors.New("invalid auth")
	}

	token := strings.Replace(auth, "Basic ", "", 1)
	data, err := base64.StdEncoding.DecodeString(token)
	if err != nil {
		return nil, errors.New("invalid auth")
	}

	authData := string(data)

	parts := strings.Split(authData, ":")
	if len(parts) != 2 {
		return nil, errors.New("invalid auth")
	}
	domainName, key := parts[0], parts[1]

	// Use API key repository for authentication
	apiKey, err := s.apiKeys.ValidateForAuth(ctx, domainName, key)
	if err != nil {
		// Always return generic error (security requirement)
		return nil, errors.New("invalid auth")
	}

	// Fetch full domain info
	domain, err := s.domains.FindByName(ctx, apiKey.Domain())
	if err != nil {
		return nil, errors.New("invalid auth")
	}

	return domain, nil
}

func validateSender(s *mailertypes.Sender, tenantDomain string) error {
	if s == nil {
		return connect.NewError(connect.CodeInvalidArgument, errors.New("sender is required"))
	}
	if err := assertHeaderSafe("sender alias", s.Alias); err != nil {
		return err
	}
	fromDomain, err := smtputils.GetEmailDomain(s.Email)
	if err != nil {
		return connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("invalid sender email %q: %w", s.Email, err))
	}
	if !senderDomainAllowed(fromDomain, tenantDomain) {
		return connect.NewError(connect.CodePermissionDenied,
			fmt.Errorf("sender domain %q is not authorized for tenant %q", fromDomain, tenantDomain))
	}
	return nil
}

// senderDomainAllowed reports whether a Sender.Email whose host is fromDomain
// is permitted for a tenant authenticated as tenantDomain. The sender domain
// is allowed when it equals the tenant domain or is a parent of it — e.g.
// tenant "k.example.com" may legitimately send from "@example.com".
func senderDomainAllowed(fromDomain, tenantDomain string) bool {
	from := strings.ToLower(strings.TrimSuffix(fromDomain, "."))
	tenant := strings.ToLower(strings.TrimSuffix(tenantDomain, "."))
	if from == "" || tenant == "" {
		return false
	}
	if from == tenant {
		return true
	}
	return strings.HasSuffix(tenant, "."+from)
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
// Policy onto a Connect code: a reserved Mode (pseudonymous) is unimplemented,
// and a Mode this build does not know is a bad argument.
//
// It answers those two `trackingpb` sentinels exactly as
// pkg/api/adminapi.trackingPolicyError does, deliberately, so that one bad Mode
// does not mean two different things depending on which API was asked; the two
// are a pair and should change together. It is named apart from that one
// because the rest of its mapping differs — anything else a send can fail on is
// the caller's argument, never a missing Domain.
func sendTrackingPolicyError(err error) error {
	switch {
	case errors.Is(err, trackingpb.ErrUnsupportedMode):
		return connect.NewError(connect.CodeUnimplemented, err)
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

func NewMailerAPIV1(db *pgxpool.Pool, backoff delivery.BackoffPolicy) mailerv1connect.MailerHandler {
	domainsCli := sqlc.NewDomainsRepository(db)
	apiKeysRepo := sqlc.NewAPIKeysRepository(db)
	apiKeysService := apikeys.NewService(apiKeysRepo)
	batchRepo := sqlc.NewBatchRepository(db)
	deliveryRepo := sqlc.NewDeliveryRepository(db, backoff)
	templatesRepo := sqlc.NewTemplatesRepository(db)

	return &mailAPIService{
		domains:    domainsCli,
		apiKeys:    apiKeysService,
		batches:    batchRepo,
		deliveries: deliveryRepo,
		templates:  templatesRepo,
		backoff:    backoff,
	}
}
