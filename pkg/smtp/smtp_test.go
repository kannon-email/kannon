package smtp

import (
	"fmt"
	"strings"
	"testing"

	st "github.com/kannon-email/kannon/proto/kannon/stats/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
)

func TestDiagnosticCode(t *testing.T) {
	msg := `------33D00BEF02924490820158550AD524A1
Content-type: text/plain; charset=Windows-1252

This is the mail system at host service.socketlabs.com.

I'm sorry to have to inform you that your message could not
be delivered to one or more recipients.

bounce-test@service.socketlabs.com :host service.socketlabs.com said: 550 No such recipient

------33D00BEF02924490820158550AD524A1
Content-Type: message/delivery-status
Content-Description: Delivery report

Reporting-MTA: smtp; service.socketlabs.com
Original-Recipient: rfc822; bounce-test@service.socketlabs.com
Action: failed
Status: 5.1.1
Diagnostic-Code: SMTP; 550 No such recipient

------33D00BEF02924490820158550AD524A1--`

	code, msg := parseCode(strings.NewReader(msg))
	assert.Equal(t, 550, code)
	assert.Equal(t, "SMTP; 550 No such recipient", msg)
}

// bounceReturnPath encodes test@test.com in batch msg_test01 on domain k.test.com.
const bounceReturnPath = "bump_dGVzdEB0ZXN0LmNvbQ==+msg_test01@k.test.com"

// dsn renders a delivery status notification carrying the given diagnostic
// code, in the multipart/report shape a real MTA sends one.
func dsn(diagnostic string) string {
	return fmt.Sprintf(`Content-Type: multipart/report; report-type=delivery-status; boundary="B"
Subject: Undelivered Mail Returned to Sender

--B
Content-Type: message/delivery-status

Reporting-MTA: smtp; mx.example.com
Final-Recipient: rfc822; test@test.com
Action: failed
%s

--B--`, diagnostic)
}

type capturingPublisher struct {
	subjects []string
	payloads [][]byte
}

func (p *capturingPublisher) Publish(subj string, data []byte) error {
	p.subjects = append(p.subjects, subj)
	p.payloads = append(p.payloads, data)
	return nil
}

func (p *capturingPublisher) lastStat(t *testing.T) *st.Stats {
	t.Helper()
	require.NotEmpty(t, p.payloads, "nothing was published")
	m := &st.Stats{}
	require.NoError(t, proto.Unmarshal(p.payloads[len(p.payloads)-1], m))
	return m
}

// An asynchronous DSN must land on the same subject as a synchronous bounce.
// It used to go to kannon.stats.soft-bounce, which no consumer subscribed to
// (#376).
func TestDataPublishesAsyncBounceOnBouncedSubject(t *testing.T) {
	pub := &capturingPublisher{}
	s := &Session{To: bounceReturnPath, nc: pub}

	require.NoError(t, s.Data(strings.NewReader(dsn("Diagnostic-Code: SMTP; 550 No such recipient"))))

	require.Equal(t, []string{"kannon.stats.bounced"}, pub.subjects)

	m := pub.lastStat(t)
	assert.Equal(t, "test@test.com", m.Email)
	assert.Equal(t, "msg_test01@k.test.com", m.MessageId)
	assert.Equal(t, "k.test.com", m.Domain)

	bounced := m.Data.GetBounced()
	require.NotNil(t, bounced, "stat must carry a Bounced payload")
	assert.Equal(t, uint32(550), bounced.Code)
	assert.Equal(t, "SMTP; 550 No such recipient", bounced.Msg)
}

// The permanent flag follows the SMTP reply class instead of being asserted
// unconditionally: a 4xx DSN means the remote MTA gave up after its own
// retries, which is terminal for us but no proof the address is dead.
func TestAsyncBouncePermanentFollowsReplyClass(t *testing.T) {
	tests := []struct {
		name       string
		diagnostic string
		wantCode   uint32
		wantPerm   bool
	}{
		{"5xx is permanent", "Diagnostic-Code: SMTP; 550 No such recipient", 550, true},
		{"4xx is not permanent", "Diagnostic-Code: SMTP; 450 Mailbox unavailable", 450, false},
		{"unparseable body falls back to 550", "X-Nothing-Here: none", 550, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pub := &capturingPublisher{}
			s := &Session{To: bounceReturnPath, nc: pub}

			require.NoError(t, s.Data(strings.NewReader(dsn(tt.diagnostic))))

			bounced := pub.lastStat(t).Data.GetBounced()
			require.NotNil(t, bounced)
			assert.Equal(t, tt.wantCode, bounced.Code)
			assert.Equal(t, tt.wantPerm, bounced.Permanent)
		})
	}
}

// A message whose recipient is not a bounce return path is not ours to report.
func TestDataIgnoresNonBounceRecipient(t *testing.T) {
	pub := &capturingPublisher{}
	s := &Session{To: "someone@example.com", nc: pub}

	require.NoError(t, s.Data(strings.NewReader(dsn("Diagnostic-Code: SMTP; 550 No such recipient"))))

	assert.Empty(t, pub.subjects, "no stat should be published")
}

func TestIsPermanentCode(t *testing.T) {
	assert.True(t, isPermanentCode(500))
	assert.True(t, isPermanentCode(550))
	assert.True(t, isPermanentCode(599))
	assert.False(t, isPermanentCode(499))
	assert.False(t, isPermanentCode(450))
	assert.False(t, isPermanentCode(600))
	assert.False(t, isPermanentCode(0))
}
