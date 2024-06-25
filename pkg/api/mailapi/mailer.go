package mailapi

import (
	"context"
	"encoding/base64"
	"fmt"
	"strings"
	"time"

	sqlc "github.com/ludusrusso/kannon/internal/db"
	"github.com/ludusrusso/kannon/internal/domains"
	"github.com/ludusrusso/kannon/internal/pool"
	"github.com/ludusrusso/kannon/internal/templates"
	"github.com/ludusrusso/kannon/internal/utils"
	pb "github.com/ludusrusso/kannon/proto/kannon/mailer/apiv1"
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

func (s mailAPIService) SendHTML(ctx context.Context, req *pb.SendHTMLReq) (*pb.SendRes, error) {
	domain, err := s.getCallDomainFromContext(ctx)
	if err != nil {
		return nil, status.Errorf(codes.Unauthenticated, "invalid or wrong auth")
	}

	req.Html = utils.ReplaceCustomFields(req.Html, req.GlobalFields)

	template, err := s.templates.CreateTransientTemplate(ctx, req.Html, domain.Domain)
	if err != nil {
		logrus.Errorf("cannot create template %v\n", err)
		return nil, status.Errorf(codes.Internal, "cannot create template %v", err)
	}

	return s.SendTemplate(ctx, &pb.SendTemplateReq{
		Sender:        req.Sender,
		Subject:       req.Subject,
		TemplateId:    template.TemplateID,
		ScheduledTime: req.ScheduledTime,
		Recipients:    req.Recipients,
		Attachments:   req.Attachments,
		GlobalFields:  nil,
	})
}

func (s mailAPIService) SendTemplate(ctx context.Context, req *pb.SendTemplateReq) (*pb.SendRes, error) {
	domain, err := s.getCallDomainFromContext(ctx)
	if err != nil {
		return nil, status.Errorf(codes.Unauthenticated, "invalid or wrong auth")
	}

	template, err := s.templates.FindTemplate(ctx, domain.Domain, req.TemplateId)
	if err != nil {
		logrus.Errorf("cannot find template %v\n", err)
		return nil, status.Errorf(codes.InvalidArgument, "cannot find template with id: %v", req.TemplateId)
	}

	template, err = s.createTemplateWithGlobalFieds(ctx, template, req.GlobalFields)
	if err != nil {
		logrus.Errorf("cannot create transient template %v\n", err)
		return nil, status.Errorf(codes.Internal, "cannot create template %v", err)
	}

	sender := pool.Sender{
		Email: req.Sender.Email,
		Alias: req.Sender.Alias,
	}

	scheduled := time.Now()
	if req.ScheduledTime != nil {
		scheduled = req.ScheduledTime.AsTime()
	}

	attachments := make(sqlc.Attachments)
	for _, r := range req.Attachments {
		attachments[r.Filename] = r.Content
	}

	pool, err := s.sendingPoll.AddRecipientsPool(ctx, template, req.Recipients, sender, scheduled, req.Subject, domain.Domain, attachments)

	if err != nil {
		logrus.Errorf("cannot create pool %v\n", err)
		return nil, err
	}

	return &pb.SendRes{
		MessageId:     pool.MessageID,
		TemplateId:    template.TemplateID,
		ScheduledTime: timestamppb.New(scheduled),
	}, nil
}

func (s mailAPIService) Close() error {
	return s.domains.Close()
}

func (s mailAPIService) createTemplateWithGlobalFieds(ctx context.Context, template sqlc.Template, globalFields map[string]string) (sqlc.Template, error) {
	if len(globalFields) == 0 {
		return template, nil
	}

	newHTML := utils.ReplaceCustomFields(template.Html, globalFields)
	if newHTML == template.Html {
		return template, nil
	}

	return s.templates.CreateTransientTemplate(ctx, newHTML, template.Domain)
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

func NewMailerAPIV1(q *sqlc.Queries) pb.MailerServer {
	domainsCli := domains.NewDomainManager(q)

	sendingPoolCli := pool.NewSendingPoolManager(q)
	templates := templates.NewTemplateManager(q)

	return &mailAPIService{
		domains:     domainsCli,
		sendingPoll: sendingPoolCli,
		templates:   templates,
	}
}
