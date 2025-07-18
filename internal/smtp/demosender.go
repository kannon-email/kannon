package smtp

import (
	"errors"
	"strings"
)

type demoSender struct {
	Hostname string
}

func (s *demoSender) Send(from, to string, msg []byte) SenderError {
	if strings.Contains(to, "error") {
		return newSMTPError(errors.New("error sending email"), true, 512)
	}
	return nil
}

func (s *demoSender) SenderName() string {
	return s.Hostname
}

func NewDemoSender(hostname string) Sender {
	return &demoSender{
		Hostname: hostname,
	}
}
