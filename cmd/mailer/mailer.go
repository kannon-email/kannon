package main

import (
	"context"
	"encoding/base64"
	"strings"
	"time"

	"github.com/sirupsen/logrus"
	log "github.com/sirupsen/logrus"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/protobuf/types/known/timestamppb"
	"gorm.io/gorm"
	"smtp.ludusrusso.space/generated/proto"
	"smtp.ludusrusso.space/internal/db"
	"smtp.ludusrusso.space/internal/domains"
	"smtp.ludusrusso.space/internal/mailer"
	"smtp.ludusrusso.space/internal/pool"
	"smtp.ludusrusso.space/internal/smtp"
	"smtp.ludusrusso.space/internal/templates"
)

// MailerService is
type MailerService struct {
	domains     domains.DomainManager
	mailer      mailer.Mailer
	templates   templates.Manager
	sendingPoll pool.SendingPoolManager
}

// SendHTML implements proto
func (s MailerService) SendHTML(ctx context.Context, in *proto.SendHTMLRequest) (*proto.SendResponse, error) {
	domain, ok := s.getCallDomainFromContext(ctx)
	if !ok {
		log.Errorf("invalid login %v\n")
		return nil, grpc.Errorf(codes.Unauthenticated, "invalid or wrong auth")
	}

	template, err := s.templates.CreateTmpTemplate(in.Html, domain.Domain)
	if err != nil {
		log.Errorf("cannot create template %v\n", err)
		return nil, grpc.Errorf(codes.Internal, "cannot create template %v", err)
	}

	sender := db.Sender{
		Email: in.Sender.Email,
		Alias: in.Sender.Alias,
	}
	pool, err := s.sendingPoll.AddPool(template, in.To, sender, in.Subject, domain.Domain)

	if err != nil {
		log.Errorf("cannot create pool %v\n", err)
		return nil, err
	}

	response := proto.SendResponse{
		MessageID:     pool.MessageID,
		TemplateID:    template.TemplateID,
		ScheduledTime: timestamppb.New(time.Now()),
	}

	return &response, nil
}

// SendTemplate implements proto
func (s MailerService) SendTemplate(ctx context.Context, in *proto.SendTemplateRequest) (*proto.SendResponse, error) {
	domain, ok := s.getCallDomainFromContext(ctx)
	if !ok {
		log.Errorf("invalid login\n")
		return nil, grpc.Errorf(codes.Unauthenticated, "invalid or wrong auth")
	}

	template, err := s.templates.FindTemplate(domain.Domain, in.TemplateID)
	if err != nil {
		log.Errorf("cannot create template %v\n", err)
		return nil, grpc.Errorf(codes.InvalidArgument, "cannot find template with id: %v", in.TemplateID)
	}

	sender := db.Sender{
		Email: in.Sender.Email,
		Alias: in.Sender.Alias,
	}
	pool, err := s.sendingPoll.AddPool(template, in.To, sender, in.Subject, domain.Domain)

	if err != nil {
		log.Errorf("cannot create pool %v\n", err)
		return nil, err
	}

	response := proto.SendResponse{
		MessageID:     pool.MessageID,
		TemplateID:    template.TemplateID,
		ScheduledTime: timestamppb.New(time.Now()),
	}

	return &response, nil
}

// Close closes all connections
func (s MailerService) Close() error {
	return s.domains.Close()
}

func (s MailerService) getCallDomainFromContext(ctx context.Context) (db.Domain, bool) {
	m, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		logrus.Debugf("Cannot find metatada\n")
		return db.Domain{}, false
	}

	auths := m.Get("authorization")
	if len(auths) != 1 {
		logrus.Debugf("Cannot find authorization header\n")
		return db.Domain{}, false
	}

	auth := auths[0]
	if !strings.HasPrefix(auth, "Basic ") {
		logrus.Debugf("No prefix Basic in auth: %v\n", auth)
		return db.Domain{}, false
	}

	token := strings.Replace(auth, "Basic ", "", 1)
	data, err := base64.StdEncoding.DecodeString(token)
	if err != nil {
		logrus.Debugf("Decode token error: %v\n", token)
		return db.Domain{}, false
	}

	authData := string(data)
	domainAndKey := strings.Split(authData, ":")
	if len(domainAndKey) != 2 {
		logrus.Debugf("Invalid token: %v\n", authData)
		return db.Domain{}, false
	}

	domain, err := s.domains.FindDomainWithKey(domainAndKey[0], domainAndKey[1])
	if err != nil {
		logrus.Debugf("Cannot find domain: %v\n", err)
		return db.Domain{}, false
	}

	return domain, true

}

func newMailerService(dbi *gorm.DB) (*MailerService, error) {
	domainsCli, err := domains.NewDomainManager(dbi)
	if err != nil {
		return nil, err
	}

	sendingPoolCli, err := pool.NewSendingPoolManager(dbi)
	if err != nil {
		return nil, err
	}

	sender := smtp.NewSender("mail.ludusrusso.space")
	mailer := mailer.NewSMTPMailer(sender, dbi)

	templates, err := templates.NewTemplateManager(dbi)
	if err != nil {
		return nil, err
	}

	return &MailerService{
		mailer:      mailer,
		domains:     domainsCli,
		sendingPoll: sendingPoolCli,
		templates:   templates,
	}, nil
}
