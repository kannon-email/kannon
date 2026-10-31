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
	"github.com/kannon-email/kannon/internal/batch"
	sqlc "github.com/kannon-email/kannon/internal/db"
	"github.com/kannon-email/kannon/internal/delivery"
	"github.com/kannon-email/kannon/internal/domains"
	"github.com/kannon-email/kannon/internal/intake"
	"github.com/kannon-email/kannon/internal/trackingpb"
	"github.com/kannon-email/kannon/internal/values"
	"github.com/kannon-email/kannon/pkg/api/connecterr"
	pb "github.com/kannon-email/kannon/proto/kannon/mailer/apiv1"
	mailerv1connect "github.com/kannon-email/kannon/proto/kannon/mailer/apiv1/apiv1connect"
	mailertypes "github.com/kannon-email/kannon/proto/kannon/mailer/types"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type mailAPIService struct {
	domains domains.Repository
	apiKeys *apikeys.Service
	intake  *intake.Service
}

func (s mailAPIService) SendHTML(ctx context.Context, req *connect.Request[pb.SendHTMLReq]) (*connect.Response[pb.SendRes], error) {
	ctx, domain, err := s.authenticate(ctx, req.Header())
	if err != nil {
		return nil, errors.New("invalid or wrong auth")
	}
	msg := &pb.SendTemplateReq{
		Sender: req.Msg.Sender, Subject: req.Msg.Subject, ScheduledTime: req.Msg.ScheduledTime,
		Recipients: req.Msg.Recipients, Attachments: req.Msg.Attachments, GlobalFields: req.Msg.GlobalFields,
		Headers: req.Msg.Headers, Tracking: req.Msg.Tracking, OneClickUnsubscribe: req.Msg.OneClickUnsubscribe,
	}
	return s.accept(ctx, domain, msg, &req.Msg.Html)
}

func (s mailAPIService) SendTemplate(ctx context.Context, req *connect.Request[pb.SendTemplateReq]) (*connect.Response[pb.SendRes], error) {
	ctx, domain, err := s.authenticate(ctx, req.Header())
	if err != nil {
		return nil, errors.New("invalid or wrong auth")
	}
	return s.accept(ctx, domain, req.Msg, nil)
}

func (s mailAPIService) accept(ctx context.Context, domain *domains.Domain, msg *pb.SendTemplateReq, html *string) (*connect.Response[pb.SendRes], error) {
	if msg.Sender == nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("sender is required"))
	}
	policy, err := trackingpb.ToPolicy(msg.Tracking)

	p := intake.Request{
		Sender: batch.Sender{Email: msg.Sender.Email, Alias: msg.Sender.Alias}, Subject: msg.Subject,
		HTML: html, TemplateID: msg.TemplateId, GlobalFields: msg.GlobalFields,
		Recipients: recipientsFromRequest(msg.Recipients), Tracking: policy, TrackingError: err,
		OneClickUnsubscribe: unsubscribeFromRequest(msg.OneClickUnsubscribe),
		Attachments:         make(batch.Attachments, len(msg.Attachments)),
	}
	if msg.Headers != nil {
		p.Headers = batch.Headers{To: msg.Headers.To, Cc: msg.Headers.Cc}
	}
	if msg.ScheduledTime != nil {
		scheduled := msg.ScheduledTime.AsTime()
		p.ScheduledTime = &scheduled
	}
	for _, a := range msg.Attachments {
		p.Attachments[a.Filename] = a.Content
	}
	result, err := s.intake.Accept(ctx, domain, p)
	if err != nil {
		return nil, sendError(err)
	}
	rejected := make([]*pb.RejectedRecipient, 0, len(result.Rejected))
	for _, r := range result.Rejected {
		rejected = append(rejected, &pb.RejectedRecipient{Email: r.Email, Reason: string(r.Reason)})
	}
	return connect.NewResponse(&pb.SendRes{
		MessageId: result.Batch.ID().String(), TemplateId: result.Batch.TemplateID(),
		ScheduledTime: timestamppb.New(result.ScheduledTime), AcceptedCount: int32(result.AcceptedCount),
		RejectedCount: int32(len(rejected)), RejectedRecipients: rejected,
	}), nil
}

func (s mailAPIService) Close() error {
	return nil
}

// recipientsFromRequest maps the Recipients of a send onto the domain type, one for
// one and in the order stated, so that everything after this line asks its questions
// of a Recipient rather than of the message that carried one (ADR 0012). The order is
// the order refusals are reported back in.
func recipientsFromRequest(rs []*mailertypes.Recipient) []intake.Recipient {
	out := make([]intake.Recipient, 0, len(rs))
	for _, r := range rs {
		// Read through the getters: a nil row is an empty row of the caller's list, and
		// is refused for having no address like any other rather than failing the send.
		policy, err := trackingpb.ToPolicy(r.GetTracking())
		out = append(out, intake.Recipient{
			Recipient: batch.Recipient{
				Email:    r.GetEmail(),
				Fields:   r.GetFields(),
				Tracking: policy,
			},
			TrackingError: err,
		})
	}
	return out
}

// unsubscribeFromRequest maps the wire type onto the domain value object. A
// caller stating nothing yields the zero value, and no unsubscribe header.
func unsubscribeFromRequest(u *mailertypes.OneClickUnsubscribe) batch.OneClickUnsubscribe {
	if u == nil {
		return batch.OneClickUnsubscribe{}
	}
	return batch.OneClickUnsubscribe{URLTemplate: u.UrlTemplate}
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

// sendError renders whatever the guarded send returned. A sender refusal keeps the code and
// the wording the old tenant comparison produced, to the letter, so no client can tell the
// mechanism changed (#438); everything else is mapped like any other application error.
func sendError(err error) error {
	var refusal *intake.SenderNotAuthorized
	if errors.As(err, &refusal) {
		return connect.NewError(connect.CodePermissionDenied,
			fmt.Errorf("sender domain %q is not authorized for tenant %q", refusal.Host, refusal.Domain.String()))
	}
	return connecterr.Error(err)
}

// NewMailerAPIV1 wires the legacy send API. maxPayload is the largest content, in bytes, a
// Batch may carry.
func NewMailerAPIV1(db *pgxpool.Pool, backoff delivery.BackoffPolicy, retryWindow time.Duration, maxPayload int) mailerv1connect.MailerHandler {
	domainsCli := sqlc.NewDomainsRepository(db)
	apiKeysRepo := sqlc.NewAPIKeysRepository(db)
	apiKeysService := apikeys.NewService(apiKeysRepo)
	return &mailAPIService{
		domains: domainsCli, apiKeys: apiKeysService,
		intake: intake.New(sqlc.NewIntakeStore(db), backoff, retryWindow, maxPayload),
	}
}
