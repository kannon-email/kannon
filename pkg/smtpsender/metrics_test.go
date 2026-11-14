package smtpsender

import (
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"

	"github.com/kannon-email/kannon/internal/envelopepb"
	"github.com/kannon-email/kannon/internal/mailflow"
	"github.com/kannon-email/kannon/internal/smtp"
)

type scriptedSender map[string]smtp.SenderError

func (s scriptedSender) SenderName() string { return "scriptedSender" }

func (s scriptedSender) Send(_, to string, _ []byte) smtp.SenderError {
	if err, ok := s[to]; ok {
		return err
	}
	return nil
}

// A transient failure with retries left is Errored, which is a send attempt and never a Delivery
// outcome; one with no retries left is a Bounced outcome that keeps the reply class.
func TestEverySMTPTransactionIsCountedWithTheOutcomeItDecides(t *testing.T) {
	reg := prometheus.NewRegistry()
	sender := scriptedSender{
		"hard@example.com":  &fakeSenderError{msg: "550 no such user", code: 550, permanent: true},
		"soft@example.com":  &fakeSenderError{msg: "451 try later", code: 451},
		"spent@example.com": &fakeSenderError{msg: "451 try later", code: 451},
	}
	s := &smtpSender{sender: sender, publisher: &recordingPublisher{}, flow: mailflow.New(reg)}

	for i, tc := range []struct {
		to          string
		shouldRetry bool
	}{
		{"ok@example.com", true},
		{"hard@example.com", true},
		{"soft@example.com", true},
		{"spent@example.com", false},
	} {
		data, err := proto.Marshal(envelopepb.FromEnvelope(envelopeTo(tc.to, tc.shouldRetry)))
		require.NoError(t, err)
		require.NoError(t, s.handleMessage(t.Context(), &fakeMsg{data: data, seq: uint64(i + 1)}))
	}

	want := `
# HELP kannon_send_attempts_total SMTP transactions run by the SMTPSender, by how they ended.
# TYPE kannon_send_attempts_total counter
kannon_send_attempts_total{result="bounced"} 2
kannon_send_attempts_total{result="delivered"} 1
kannon_send_attempts_total{result="errored"} 1
# HELP kannon_delivery_outcomes_total Delivery outcomes, counted by the Component that decides them. source and permanent are set only for bounced.
# TYPE kannon_delivery_outcomes_total counter
kannon_delivery_outcomes_total{outcome="bounced",permanent="false",source="sync"} 1
kannon_delivery_outcomes_total{outcome="bounced",permanent="true",source="sync"} 1
kannon_delivery_outcomes_total{outcome="delivered",permanent="",source=""} 1
`
	assert.NoError(t, testutil.GatherAndCompare(reg, strings.NewReader(want), "kannon_send_attempts_total", "kannon_delivery_outcomes_total"))

	timed, err := testutil.GatherAndCount(reg, "kannon_smtp_send_duration_seconds")
	require.NoError(t, err)
	assert.Equal(t, 3, timed, "one duration series per result")
}
