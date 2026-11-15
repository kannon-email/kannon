package e2e_test

import (
	"fmt"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/kannon-email/kannon/internal/smtp"
	"github.com/stretchr/testify/require"
)

// capturingMX stands in for every remote MX through the Sender startup option. It records each
// attempt, and answers according to the local-part of the Recipient address, so a scenario steers
// the remote side from the address it sends to:
//
//	bounce.<suffix>         -> 550 on every attempt (permanent)
//	transient.x<N>.<suffix> -> 451 on attempts 1..N, accepted on attempt N+1
//	deferred.<suffix>       -> 451 on every attempt (never accepted)
//	anything else           -> accepted
//
// The grammar is e2e vocabulary, not domain language.
type capturingMX struct {
	mu       sync.Mutex
	attempts map[string][]envelope
	accepted map[string]envelope
}

// envelope is one transmission as the remote MX saw it: the return path given as envelope-from, the
// envelope Recipient and the raw message.
type envelope struct {
	ReturnPath string
	To         string
	Body       []byte
}

func newCapturingMX() *capturingMX {
	return &capturingMX{attempts: map[string][]envelope{}, accepted: map[string]envelope{}}
}

func (m *capturingMX) SenderName() string {
	return "capturing-mx"
}

func (m *capturingMX) Send(from, to string, body []byte) smtp.SenderError {
	m.mu.Lock()
	defer m.mu.Unlock()

	e := envelope{ReturnPath: from, To: to, Body: body}
	m.attempts[to] = append(m.attempts[to], e)
	n := len(m.attempts[to])

	local, _, _ := strings.Cut(to, "@")
	switch {
	case strings.HasPrefix(local, "bounce."):
		return &mxReply{code: 550, permanent: true, msg: "550 user unknown: " + to}
	case strings.HasPrefix(local, "deferred."):
		return &mxReply{code: 451, msg: fmt.Sprintf("451 try again later (attempt %d): %s", n, to)}
	}
	if failures, ok := transientFailures(local); ok && n <= failures {
		return &mxReply{code: 451, msg: fmt.Sprintf("451 transient failure (attempt %d/%d): %s", n, failures, to)}
	}
	m.accepted[to] = e
	return nil
}

// Attempts is how many times Kannon tried to transmit to the Recipient, answered or not.
func (m *capturingMX) Attempts(rcpt string) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.attempts[rcpt])
}

// Envelope is the message the MX accepted for the Recipient, if it accepted one.
func (m *capturingMX) Envelope(rcpt string) (envelope, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	e, ok := m.accepted[rcpt]
	return e, ok
}

// AwaitEnvelope waits until the MX has accepted a message for the Recipient and returns it parsed.
func (m *capturingMX) AwaitEnvelope(t *testing.T, rcpt string) ParsedEmail {
	t.Helper()
	var parsed ParsedEmail
	eventually(t, "an Envelope accepted for "+rcpt, func(c *probe) {
		e, ok := m.Envelope(rcpt)
		require.True(c, ok, "no Envelope accepted for %s", rcpt)
		parsed = parseEmail(c, e.Body)
		parsed.ReturnPath = e.ReturnPath
	})
	return parsed
}

func transientFailures(local string) (int, bool) {
	rest, ok := strings.CutPrefix(local, "transient.x")
	if !ok {
		return 0, false
	}
	count, _, ok := strings.Cut(rest, ".")
	if !ok {
		return 0, false
	}
	n, err := strconv.Atoi(count)
	return n, err == nil && n >= 1
}

type mxReply struct {
	code      uint32
	permanent bool
	msg       string
}

func (r *mxReply) Error() string     { return r.msg }
func (r *mxReply) IsPermanent() bool { return r.permanent }
func (r *mxReply) Code() uint32      { return r.code }
