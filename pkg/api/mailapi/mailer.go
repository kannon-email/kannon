package mailapi

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/http"
	"strings"
	"time"

	"connectrpc.com/connect"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/kannon-email/kannon/internal/apikeys"
	sqlc "github.com/kannon-email/kannon/internal/db"
	"github.com/kannon-email/kannon/internal/domains"
	"github.com/kannon-email/kannon/internal/pool"
	smtputils "github.com/kannon-email/kannon/internal/smtp"
	"github.com/kannon-email/kannon/internal/templates"
	"github.com/kannon-email/kannon/internal/utils"
	pb "github.com/kannon-email/kannon/proto/kannon/mailer/apiv1"
	mailerv1connect "github.com/kannon-email/kannon/proto/kannon/mailer/apiv1/apiv1connect"
	mailertypes "github.com/kannon-email/kannon/proto/kannon/mailer/types"
	"github.com/sirupsen/logrus"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type mailAPIService struct {
	domains     domains.DomainManager
	apiKeys     *apikeys.Service
	templates   templates.Manager
	sendingPool pool.SendingPoolManager
}

func (s mailAPIService) SendHTML(ctx context.Context, req *connect.Request[pb.SendHTMLReq]) (*connect.Response[pb.SendRes], error) {
	domain, err := s.getCallDomainFromHeaders(ctx, req.Header())
	if err != nil {
		return nil, fmt.Errorf("invalid or wrong auth")
	}

	req.Msg.Html = utils.ReplaceCustomFields(req.Msg.Html, req.Msg.GlobalFields)

	template, err := s.templates.CreateTransientTemplate(ctx, req.Msg.Html, domain.Domain)
	if err != nil {
		logrus.Errorf("cannot create template %v\n", err)
		return nil, fmt.Errorf("cannot create template %v", err)
	}

	res := &pb.SendTemplateReq{
		Sender:        req.Msg.Sender,
		Subject:       req.Msg.Subject,
		TemplateId:    template.TemplateID,
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
		return nil, fmt.Errorf("invalid or wrong auth")
	}

	return s.sendTemplate(ctx, domain, req)
}

func (s mailAPIService) sendTemplate(ctx context.Context, domain sqlc.Domain, req *connect.Request[pb.SendTemplateReq]) (*connect.Response[pb.SendRes], error) {
	template, err := s.templates.FindTemplate(ctx, domain.Domain, req.Msg.TemplateId)
	if err != nil {
		logrus.Errorf("cannot find template %v\n", err)
		return nil, fmt.Errorf("cannot find template with id: %v", req.Msg.TemplateId)
	}

	template, err = s.createTemplateWithGlobalFields(ctx, template, req.Msg.GlobalFields)
	if err != nil {
		logrus.Errorf("cannot create transient template %v\n", err)
		return nil, fmt.Errorf("cannot create template %v", err)
	}

	sender := pool.Sender{
		Email: req.Msg.Sender.Email,
		Alias: req.Msg.Sender.Alias,
	}

	scheduled := time.Now()
	if req.Msg.ScheduledTime != nil {
		scheduled = req.Msg.ScheduledTime.AsTime()
	}

	attachments := make(sqlc.Attachments)
	for _, r := range req.Msg.Attachments {
		attachments[r.Filename] = r.Content
	}

	customHeaders, err := validateHeaders(req.Msg.Headers)
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}

	pool, err := s.sendingPool.AddRecipientsPool(ctx, template, req.Msg.Recipients, sender, scheduled, req.Msg.Subject, domain.Domain, attachments, customHeaders)

	if err != nil {
		logrus.Errorf("cannot create pool %v\n", err)
		return nil, err
	}

	return connect.NewResponse(&pb.SendRes{
		MessageId:     pool.MessageID,
		TemplateId:    template.TemplateID,
		ScheduledTime: timestamppb.New(scheduled),
	}), nil
}

func (s mailAPIService) Close() error {
	return s.domains.Close()
}

func (s mailAPIService) createTemplateWithGlobalFields(ctx context.Context, template sqlc.Template, globalFields map[string]string) (sqlc.Template, error) {
	if len(globalFields) == 0 {
		return template, nil
	}

	newHTML := utils.ReplaceCustomFields(template.Html, globalFields)
	if newHTML == template.Html {
		return template, nil
	}

	return s.templates.CreateTransientTemplate(ctx, newHTML, template.Domain)
}

func (s mailAPIService) getCallDomainFromHeaders(ctx context.Context, headers http.Header) (sqlc.Domain, error) {
	auth := headers.Get("Authorization")

	if !strings.HasPrefix(auth, "Basic ") {
		return sqlc.Domain{}, fmt.Errorf("invalid auth")
	}

	token := strings.Replace(auth, "Basic ", "", 1)
	data, err := base64.StdEncoding.DecodeString(token)
	if err != nil {
		return sqlc.Domain{}, fmt.Errorf("invalid auth")
	}

	authData := string(data)

	parts := strings.Split(authData, ":")
	if len(parts) != 2 {
		return sqlc.Domain{}, fmt.Errorf("invalid auth")
	}
	domainName, key := parts[0], parts[1]

	// Use API key repository for authentication
	apiKey, err := s.apiKeys.ValidateForAuth(ctx, domainName, key)
	if err != nil {
		// Always return generic error (security requirement)
		return sqlc.Domain{}, fmt.Errorf("invalid auth")
	}

	// Fetch full domain info
	domain, err := s.domains.FindDomain(ctx, apiKey.Domain())
	if err != nil {
		return sqlc.Domain{}, fmt.Errorf("invalid auth")
	}

	return domain, nil
}

func validateHeaders(h *mailertypes.Headers) (sqlc.Headers, error) {
	if h == nil {
		return sqlc.Headers{}, nil
	}
	for _, email := range h.To {
		if !smtputils.Validate(email) {
			return sqlc.Headers{}, fmt.Errorf("invalid To header: %q is not a valid email address", email)
		}
	}
	for _, email := range h.Cc {
		if !smtputils.Validate(email) {
			return sqlc.Headers{}, fmt.Errorf("invalid Cc header: %q is not a valid email address", email)
		}
	}
	return sqlc.Headers{To: h.To, Cc: h.Cc}, nil
}

func NewMailerAPIV1(q *sqlc.Queries, db *pgxpool.Pool) mailerv1connect.MailerHandler {
	domainsCli := domains.NewDomainManager(q)
	apiKeysRepo := sqlc.NewAPIKeysRepository(q, db)
	apiKeysService := apikeys.NewService(apiKeysRepo)
	sendingPoolCli := pool.NewSendingPoolManager(q)
	templates := templates.NewTemplateManager(q)

	return &mailAPIService{
		domains:     domainsCli,
		apiKeys:     apiKeysService,
		sendingPool: sendingPoolCli,
		templates:   templates,
	}
}
