package main

import (
	"context"
	"database/sql"
	"encoding/base64"
	"fmt"
	"strings"
	"time"

	"github.com/sirupsen/logrus"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
	"kannon.gyozatech.dev/generated/pb"
	"kannon.gyozatech.dev/generated/sqlc"
	"kannon.gyozatech.dev/internal/domains"
	"kannon.gyozatech.dev/internal/pool"
	"kannon.gyozatech.dev/internal/templates"
)

type service struct {
	domains     domains.DomainManager
	templates   templates.Manager
	sendingPoll pool.SendingPoolManager
}

func (s service) SendHTML(ctx context.Context, in *pb.SendHTMLRequest) (*pb.SendResponse, error) {
	domain, ok := s.getCallDomainFromContext(ctx)
	if !ok {
		logrus.Errorf("invalid login\n")
		return nil, status.Errorf(codes.Unauthenticated, "invalid or wrong auth")
	}

	template, err := s.templates.CreateTemplate(in.Html, domain.Domain)
	if err != nil {
		logrus.Errorf("cannot create template %v\n", err)
		return nil, status.Errorf(codes.Internal, "cannot create template %v", err)
	}

	sender := pool.Sender{
		Email: in.Sender.Email,
		Alias: in.Sender.Alias,
	}
	pool, err := s.sendingPoll.AddPool(template, in.To, sender, in.Subject, domain.Domain)

	if err != nil {
		logrus.Errorf("cannot create pool %v\n", err)
		return nil, err
	}

	response := pb.SendResponse{
		MessageId:     pool.MessageID,
		TemplateId:    template.TemplateID,
		ScheduledTime: timestamppb.New(time.Now()),
	}

	return &response, nil
}

func (s service) SendTemplate(ctx context.Context, in *pb.SendTemplateRequest) (*pb.SendResponse, error) {
	domain, ok := s.getCallDomainFromContext(ctx)
	if !ok {
		logrus.Errorf("invalid login\n")
		return nil, status.Errorf(codes.Unauthenticated, "invalid or wrong auth")
	}

	template, err := s.templates.FindTemplate(domain.Domain, in.TemplateId)
	if err != nil {
		logrus.Errorf("cannot create template %v\n", err)
		return nil, status.Errorf(codes.InvalidArgument, "cannot find template with id: %v", in.TemplateId)
	}

	sender := pool.Sender{
		Email: in.Sender.Email,
		Alias: in.Sender.Alias,
	}
	pool, err := s.sendingPoll.AddPool(template, in.To, sender, in.Subject, domain.Domain)

	if err != nil {
		logrus.Errorf("cannot create pool %v\n", err)
		return nil, err
	}

	response := pb.SendResponse{
		MessageId:     pool.MessageID,
		TemplateId:    template.TemplateID,
		ScheduledTime: timestamppb.New(time.Now()),
	}

	return &response, nil
}

func (s service) Close() error {
	return s.domains.Close()
}

func (s service) getCallDomainFromContext(ctx context.Context) (sqlc.Domain, bool) {
	m, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		logrus.Debugf("Cannot find metatada\n")
		return sqlc.Domain{}, false
	}

	auths := m.Get("authorization")
	if len(auths) != 1 {
		logrus.Debugf("Cannot find authorization header\n")
		return sqlc.Domain{}, false
	}

	auth := auths[0]
	if !strings.HasPrefix(auth, "Basic ") {
		logrus.Debugf("No prefix Basic in auth: %v\n", auth)
		return sqlc.Domain{}, false
	}

	token := strings.Replace(auth, "Basic ", "", 1)
	data, err := base64.StdEncoding.DecodeString(token)
	if err != nil {
		logrus.Debugf("Decode token error: %v\n", token)
		return sqlc.Domain{}, false
	}

	authData := string(data)

	var d, k string
	_, err = fmt.Sscanf(authData, "%v:%v", &d, &k)
	if err != nil {
		logrus.Debugf("Invalid token: %v\n", authData)
		return sqlc.Domain{}, false
	}

	domain, err := s.domains.FindDomainWithKey(d, k)
	if err != nil {
		logrus.Debugf("Cannot find domain: %v\n", err)
		return sqlc.Domain{}, false
	}

	return domain, true

}

func newMailerService(dbi *sql.DB) (pb.MailerServer, error) {
	domainsCli, err := domains.NewDomainManager(dbi)
	if err != nil {
		return nil, err
	}

	sendingPoolCli, err := pool.NewSendingPoolManager(dbi)
	if err != nil {
		return nil, err
	}

	templates, err := templates.NewTemplateManager(dbi)
	if err != nil {
		return nil, err
	}

	return &service{
		domains:     domainsCli,
		sendingPoll: sendingPoolCli,
		templates:   templates,
	}, nil
}
