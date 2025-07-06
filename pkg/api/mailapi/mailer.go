package mailapi

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/http"
	"strings"
	"time"

	"connectrpc.com/connect"
	sqlc "github.com/ludusrusso/kannon/internal/db"
	"github.com/ludusrusso/kannon/internal/domains"
	"github.com/ludusrusso/kannon/internal/pool"
	"github.com/ludusrusso/kannon/internal/templates"
	"github.com/ludusrusso/kannon/internal/utils"
	pb "github.com/ludusrusso/kannon/proto/kannon/mailer/apiv1"
	mailerv1connect "github.com/ludusrusso/kannon/proto/kannon/mailer/apiv1/apiv1connect"
	"github.com/sirupsen/logrus"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type mailAPIService struct {
	domains     domains.DomainManager
	templates   templates.Manager
	sendingPoll pool.SendingPoolManager
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

	pool, err := s.sendingPoll.AddRecipientsPool(ctx, template, req.Msg.Recipients, sender, scheduled, req.Msg.Subject, domain.Domain, attachments)

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
		return sqlc.Domain{}, fmt.Errorf("no prefix Basic in auth: %v", auth)
	}

	token := strings.Replace(auth, "Basic ", "", 1)
	data, err := base64.StdEncoding.DecodeString(token)
	if err != nil {
		return sqlc.Domain{}, fmt.Errorf("decode token error: %v", token)
	}

	authData := string(data)

	parts := strings.Split(authData, ":")
	d, k := parts[0], parts[1]

	domain, err := s.domains.FindDomainWithKey(ctx, d, k)
	if err != nil {
		return sqlc.Domain{}, fmt.Errorf("cannot find domain: %w", err)
	}

	return domain, nil
}

func NewMailerAPIV1(q *sqlc.Queries) mailerv1connect.MailerHandler {
	domainsCli := domains.NewDomainManager(q)

	sendingPoolCli := pool.NewSendingPoolManager(q)
	templates := templates.NewTemplateManager(q)

	return &mailAPIService{
		domains:     domainsCli,
		sendingPoll: sendingPoolCli,
		templates:   templates,
	}
}
