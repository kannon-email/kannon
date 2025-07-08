package e2e_test

import (
	"sync"

	"github.com/kannon-email/kannon/internal/smtp"
)

type senderMock struct {
	mu   sync.RWMutex
	msgs map[string]msg
}

func (s *senderMock) SenderName() string {
	return "senderMock"
}

type msg struct {
	From string
	To   string
	Body []byte
}

func (s *senderMock) Send(from string, to string, body []byte) smtp.SenderError {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.msgs == nil {
		s.msgs = make(map[string]msg)
	}

	s.msgs[to] = msg{From: from, To: to, Body: body}
	return nil
}

func (s *senderMock) GetEmail(to string) *msg {
	s.mu.RLock()
	defer s.mu.RUnlock()
	m, ok := s.msgs[to]
	if !ok {
		return nil
	}
	return &m
}
