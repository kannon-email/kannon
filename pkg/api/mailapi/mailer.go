package mailapi

import (
	"context"
	"encoding/base64"
	"fmt"
	"strings"
	"time"

	"github.com/ludusrusso/kannon/generated/pb"
	sqlc "github.com/ludusrusso/kannon/internal/db"
	"github.com/ludusrusso/kannon/internal/domains"
	"github.com/ludusrusso/kannon/internal/pool"
	"github.com/ludusrusso/kannon/internal/templates"
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

	template, err := s.templates.CreateTemplate(ctx, req.Html, domain.Domain)
	if err != nil {
		logrus.Errorf("cannot create template %v\n", err)
		return nil, status.Errorf(codes.Internal, "cannot create template %v", err)
	}

	return s.SendTemplate(ctx, &pb.SendTemplateReq{
		Sender:        req.Sender,
		To:            req.To,
		Subject:       req.Subject,
		TemplateId:    template.TemplateID,
		ScheduledTime: req.ScheduledTime,
	})
}

func (s mailAPIService) SendTemplate(ctx context.Context, req *pb.SendTemplateReq) (*pb.SendRes, error) {
	domain, err := s.getCallDomainFromContext(ctx)
	if err != nil {
		return nil, status.Errorf(codes.Unauthenticated, "invalid or wrong auth")
	}
	template, err := s.templates.FindTemplate(ctx, domain.Domain, req.TemplateId)
	if err != nil {
		logrus.Errorf("cannot create template %v\n", err)
		return nil, status.Errorf(codes.InvalidArgument, "cannot find template with id: %v", req.TemplateId)
	}

	sender := pool.Sender{
		Email: req.Sender.Email,
		Alias: req.Sender.Alias,
	}

	scheduled := time.Now()
	if req.ScheduledTime != nil {
		scheduled = req.ScheduledTime.AsTime()
	}

	pool, err := s.sendingPoll.AddPool(ctx, template, req.To, sender, scheduled, req.Subject, domain.Domain)

	if err != nil {
		logrus.Errorf("cannot create pool %v\n", err)
		return nil, err
	}

	Res := pb.SendRes{
		MessageId:     pool.MessageID,
		TemplateId:    template.TemplateID,
		ScheduledTime: timestamppb.New(time.Now()),
	}

	return &Res, nil
}

func (s mailAPIService) Close() error {
	return s.domains.Close()
}

func (s mailAPIService) getCallDomainFromContext(ctx context.Context) (sqlc.Domain, error) {
	m, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return sqlc.Domain{}, fmt.Errorf("cannot find metatada")
	}
	logrus.Infof("ok:%v", ok)

	auths := m.Get("authorization")
	if len(auths) != 1 {
		return sqlc.Domain{}, fmt.Errorf("cannot find authorization header")
	}

	auth := auths[0]
	if !strings.HasPrefix(auth, "Basic ") {
		return sqlc.Domain{}, fmt.Errorf("no prefix Basic in auth: %v", auth)
	}
	logrus.Infof("auth: %v", auth)

	token := strings.Replace(auth, "Basic ", "", 1)
	data, err := base64.StdEncoding.DecodeString(token)
	if err != nil {
		return sqlc.Domain{}, fmt.Errorf("decode token error: %v", token)
	}
	logrus.Infof("token: %v, data: %v", token, data)

	authData := string(data)
	logrus.Infof("authData: %v", authData)

	parts := strings.Split(authData, ":")
	d, k := parts[0], parts[1]

	logrus.Infof("domain: %v, key: %v", d, k)

	domain, err := s.domains.FindDomainWithKey(ctx, d, k)
	if err != nil {
		return sqlc.Domain{}, fmt.Errorf("cannot find domain: %w", err)
	}

	return domain, nil
}

func NewMailAPIService(q *sqlc.Queries) pb.MailerServer {
	domainsCli := domains.NewDomainManager(q)

	sendingPoolCli := pool.NewSendingPoolManager(q)
	templates := templates.NewTemplateManager(q)

	return &mailAPIService{
		domains:     domainsCli,
		sendingPoll: sendingPoolCli,
		templates:   templates,
	}
}
