package e2e_test

import (
	"fmt"
	"strconv"
	"strings"
	"sync"

	"github.com/kannon-email/kannon/internal/smtp"
)

// senderMock is a test double for smtp.Sender used by the e2e harness. It
// parses the local-part of the Recipient address against a magic-address
// grammar so subtests can declaratively trigger per-Recipient failure
// behaviour without touching helper code:
//
//	bounce.<suffix>           -> permanent SenderError, code 550
//	transient.x<N>.<suffix>   -> transient SenderError on attempts 1..N,
//	                             success on attempt N+1
//	(anything else)           -> success, captured for inspection
//
// The grammar is e2e-internal vocabulary and intentionally not promoted
// to CONTEXT.md.
type senderMock struct {
	mu       sync.RWMutex
	latest   map[string]msg
	attempts map[string]int
	history  map[string][]msg
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
	s.ensureInit()

	s.attempts[to]++
	m := msg{From: from, To: to, Body: body}
	s.history[to] = append(s.history[to], m)

	if isBounceAddress(to) {
		return &mockSenderError{
			err:       fmt.Errorf("550 user unknown: %s", to),
			permanent: true,
			code:      550,
		}
	}

	if n, ok := transientFailCount(to); ok && s.attempts[to] <= n {
		return &mockSenderError{
			err:       fmt.Errorf("451 transient failure (attempt %d/%d): %s", s.attempts[to], n, to),
			permanent: false,
			code:      451,
		}
	}

	s.latest[to] = m
	return nil
}

// GetEmail returns the most recent successfully-sent Envelope for the
// given Recipient, or nil if none. Failed attempts (e.g. bounce.*) do not
// populate this map.
func (s *senderMock) GetEmail(to string) *msg {
	s.mu.RLock()
	defer s.mu.RUnlock()
	m, ok := s.latest[to]
	if !ok {
		return nil
	}
	return &m
}

// History returns every Envelope the SMTPSender attempted to deliver to
// the given Recipient, in attempt order. Includes both successful and
// failed attempts.
func (s *senderMock) History(to string) []msg {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if h, ok := s.history[to]; ok {
		out := make([]msg, len(h))
		copy(out, h)
		return out
	}
	return nil
}

func (s *senderMock) ensureInit() {
	if s.latest == nil {
		s.latest = make(map[string]msg)
	}
	if s.attempts == nil {
		s.attempts = make(map[string]int)
	}
	if s.history == nil {
		s.history = make(map[string][]msg)
	}
}

func isBounceAddress(to string) bool {
	at := strings.IndexByte(to, '@')
	if at < 0 {
		return false
	}
	return strings.HasPrefix(to[:at], "bounce.")
}

// transientFailCount decodes N from a `transient.x<N>.<suffix>` local-part.
// Returns (N, true) when the address matches the grammar with N >= 1.
func transientFailCount(to string) (int, bool) {
	at := strings.IndexByte(to, '@')
	if at < 0 {
		return 0, false
	}
	local := to[:at]
	const prefix = "transient.x"
	if !strings.HasPrefix(local, prefix) {
		return 0, false
	}
	rest := local[len(prefix):]
	dot := strings.IndexByte(rest, '.')
	if dot <= 0 {
		return 0, false
	}
	n, err := strconv.Atoi(rest[:dot])
	if err != nil || n < 1 {
		return 0, false
	}
	return n, true
}

type mockSenderError struct {
	err       error
	permanent bool
	code      uint32
}

func (e *mockSenderError) Error() string     { return e.err.Error() }
func (e *mockSenderError) IsPermanent() bool { return e.permanent }
func (e *mockSenderError) Code() uint32      { return e.code }
