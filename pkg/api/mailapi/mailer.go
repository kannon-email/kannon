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
	"github.com/kannon-email/kannon/internal/utils"
	pb "github.com/kannon-email/kannon/proto/kannon/mailer/apiv1"
	mailerv1connect "github.com/kannon-email/kannon/proto/kannon/mailer/apiv1/apiv1connect"
	mailertypes "github.com/kannon-email/kannon/proto/kannon/mailer/types"
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

	b, err := batch.New(domain.Domain(), req.Msg.Subject, sender, template.TemplateID(), attachments, customHeaders)
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}

	if err := s.scheduleBatch(ctx, b, req.Msg.Recipients, scheduled); err != nil {
		slog.Error("cannot create pool", "err", err)
		return nil, err
	}

	return connect.NewResponse(&pb.SendRes{
		MessageId:     b.ID().String(),
		TemplateId:    template.TemplateID(),
		ScheduledTime: timestamppb.New(scheduled),
	}), nil
}

func (s mailAPIService) Close() error {
	return nil
}

func (s mailAPIService) scheduleBatch(ctx context.Context, b *batch.Batch, recipients []*mailertypes.Recipient, scheduled time.Time) error {
	if err := s.batches.Create(ctx, b); err != nil {
		return err
	}
	ds := make([]*delivery.Delivery, 0, len(recipients))
	for _, r := range recipients {
		if strings.TrimSpace(r.Email) == "" {
			slog.Warn("skipping recipient with empty email in batch " + b.ID().String())
			continue
		}
		d, err := delivery.New(delivery.NewParams{
			BatchID:       b.ID(),
			Email:         r.Email,
			Fields:        r.Fields,
			Domain:        b.Domain(),
			ScheduledTime: scheduled,
			Backoff:       s.backoff,
		})
		if err != nil {
			slog.Warn(fmt.Sprintf("skipping recipient %q in batch %s: %v", r.Email, b.ID().String(), err))
			continue
		}
		ds = append(ds, d)
	}
	if len(ds) == 0 {
		return nil
	}
	if err := s.deliveries.Schedule(ctx, ds...); err != nil {
		return fmt.Errorf("cannot schedule deliveries for batch %s: %w", b.ID().String(), err)
	}
	return nil
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
