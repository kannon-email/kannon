package smtp

import (
	"context"
	"io"
	"io/ioutil"
	"log"
	"time"

	"github.com/emersion/go-smtp"
)

// The Backend implements SMTP server methods.
type Backend struct{}

func (bkd *Backend) AnonymousLogin(_ *smtp.ConnectionState) (smtp.Session, error) {
	return &Session{}, nil
}

func (bkd *Backend) Login(_ *smtp.ConnectionState, _, _ string) (smtp.Session, error) {
	return nil, smtp.ErrAuthUnsupported
}

// A Session is returned after EHLO.
type Session struct{}

func (s *Session) Mail(from string, opts smtp.MailOptions) error {
	log.Println("Mail from:", from)
	return nil
}

func (s *Session) Rcpt(to string) error {
	log.Println("Rcpt to:", to)
	return nil
}

func (s *Session) Data(r io.Reader) error {
	if b, err := ioutil.ReadAll(r); err != nil {
		return err
	} else {
		log.Println("Data:", string(b))
	}
	return nil
}

func (s *Session) Reset() {}

func (s *Session) Logout() error {
	return nil
}

func Run(ctx context.Context) {

	// addr := viper.GetString("smtp.address")
	// domain := viper.GetString("smtp.domain")

	be := &Backend{}

	s := smtp.NewServer(be)

	s.Addr = ":1025"
	s.Domain = "localhost"
	s.ReadTimeout = 10 * time.Second
	s.WriteTimeout = 10 * time.Second
	s.MaxMessageBytes = 1024 * 1024
	s.MaxRecipients = 50
	s.AllowInsecureAuth = true

	go func() {
		log.Println("Starting server at", s.Addr)
		if err := s.ListenAndServe(); err != nil {
			log.Fatal(err)
		}
	}()

	<-ctx.Done()
	s.Close()
}
