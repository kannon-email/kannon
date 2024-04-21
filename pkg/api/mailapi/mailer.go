package mailapi

import (
	"context"
	"encoding/base64"
	"fmt"
	"strings"
	"time"

	"connectrpc.com/connect"
	sqlc "github.com/ludusrusso/kannon/internal/db"
	"github.com/ludusrusso/kannon/internal/domains"
	"github.com/ludusrusso/kannon/internal/pool"
	"github.com/ludusrusso/kannon/internal/templates"
	"github.com/ludusrusso/kannon/proto/kannon/mailer/apiv1"
	pb "github.com/ludusrusso/kannon/proto/kannon/mailer/apiv1"
	cn "github.com/ludusrusso/kannon/proto/kannon/mailer/apiv1/apiv1connect"
	"github.com/sirupsen/logrus"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type mailAPIService struct {
	domains     domains.DomainManager
	templates   templates.Manager
	sendingPoll pool.SendingPoolManager
}

func (s mailAPIService) SendHTML(ctx context.Context, req *connect.Request[apiv1.SendHTMLReq]) (*connect.Response[apiv1.SendRes], error) {
	domain, err := s.getCallDomainFromContext(ctx)
	if err != nil {
		return nil, status.Errorf(codes.Unauthenticated, "invalid or wrong auth")
	}

	template, err := s.templates.CreateTransientTemplate(ctx, req.Msg.Html, domain.Domain)
	if err != nil {
		logrus.Errorf("cannot create template %v\n", err)
		return nil, status.Errorf(codes.Internal, "cannot create template %v", err)
	}

	res := connect.NewRequest(&pb.SendTemplateReq{
		Sender:        req.Msg.Sender,
		Subject:       req.Msg.Subject,
		TemplateId:    template.TemplateID,
		ScheduledTime: req.Msg.ScheduledTime,
		Recipients:    req.Msg.Recipients,
		Attachments:   req.Msg.Attachments,
	})

	return s.SendTemplate(ctx, res)
}

func (s mailAPIService) SendTemplate(ctx context.Context, req *connect.Request[apiv1.SendTemplateReq]) (*connect.Response[apiv1.SendRes], error) {
	domain, err := s.getCallDomainFromContext(ctx)
	if err != nil {
		return nil, status.Errorf(codes.Unauthenticated, "invalid or wrong auth")
	}
	template, err := s.templates.FindTemplate(ctx, domain.Domain, req.Msg.TemplateId)
	if err != nil {
		logrus.Errorf("cannot create template %v\n", err)
		return nil, status.Errorf(codes.InvalidArgument, "cannot find template with id: %v", req.Msg.TemplateId)
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

	res := connect.NewResponse(&pb.SendRes{
		MessageId:     pool.MessageID,
		TemplateId:    template.TemplateID,
		ScheduledTime: timestamppb.New(scheduled),
	})

	return res, nil
}

func (s mailAPIService) Close() error {
	return s.domains.Close()
}

func (s mailAPIService) getCallDomainFromContext(ctx context.Context) (sqlc.Domain, error) {
	m, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return sqlc.Domain{}, fmt.Errorf("cannot find metatada")
	}

	auths := m.Get("authorization")
	if len(auths) != 1 {
		return sqlc.Domain{}, fmt.Errorf("cannot find authorization header")
	}

	auth := auths[0]
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

func NewMailerAPIV1(q *sqlc.Queries) cn.MailerHandler {
	domainsCli := domains.NewDomainManager(q)

	sendingPoolCli := pool.NewSendingPoolManager(q)
	templates := templates.NewTemplateManager(q)

	return &mailAPIService{
		domains:     domainsCli,
		sendingPoll: sendingPoolCli,
		templates:   templates,
	}
}
