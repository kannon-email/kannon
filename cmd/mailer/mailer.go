package main

import (
	"context"

	log "github.com/sirupsen/logrus"
	"smtp.ludusrusso.space/generated/proto"
	"smtp.ludusrusso.space/internal/db"
	"smtp.ludusrusso.space/internal/domains"
	"smtp.ludusrusso.space/internal/mailer"
	"smtp.ludusrusso.space/internal/pool"
	"smtp.ludusrusso.space/internal/smtp"
)

// MailerService is
type MailerService struct {
	domains     domains.DomainManager
	mailer      mailer.Mailer
	sendingPoll pool.SendingPoolManager
}

// SendHTML implements proto
func (s MailerService) SendHTML(ctx context.Context, in *proto.SendHTMLRequest) (*proto.SendHTMLResponse, error) {
	domain, err := s.domains.FindDomain("mail.ludusrusso.space")
	if err != nil {
		log.Errorf("cannot find domain %v\n", err)
		return nil, err
	}

	template, err := s.sendingPoll.CreateTemplate(in.Html, db.TemplateTypePermanent)
	if err != nil {
		log.Errorf("cannot create template %v\n", err)
		return nil, err
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

	response := proto.SendHTMLResponse{
		MessageID: pool.MessageID,
	}

	return &response, nil
}

// Close closes all connections
func (s MailerService) Close() error {
	return s.domains.Close()
}

func newMailerService() (*MailerService, error) {
	db, err := db.NewDb(true)
	if err != nil {
		return nil, err
	}
	log.Printf("Connected to db\n")
	domainsCli, err := domains.NewDomainManager(db)
	if err != nil {
		return nil, err
	}

	sendingPoolCli, err := pool.NewSendingPoolManager(db)
	if err != nil {
		return nil, err
	}

	sender := smtp.NewSender("mail.ludusrusso.space")
	mailer := mailer.NewSMTPMailer(sender)

	return &MailerService{
		mailer:      mailer,
		domains:     domainsCli,
		sendingPoll: sendingPoolCli,
	}, nil
}
