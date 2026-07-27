package smtpsender

import (
	"context"
	"encoding/base64"
	"sync"
	"testing"
	"time"

	"github.com/kannon-email/kannon/internal/smtp"
	"github.com/kannon-email/kannon/internal/tests"
	"github.com/kannon-email/kannon/internal/utils"
	msgtypes "github.com/kannon-email/kannon/proto/kannon/mailer/types"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
)

// The sending consumer acks only once the SMTP transaction has returned, so
// the deadline it is given is the deadline of a real email delivery. #396 set
// a BackOff curve on every consumer, and BackOff overrides AckWait: the
// deadline silently became one second and every slower send went out twice
// (#425). This is that regression, expressed against a real server — the
// consumer is read back from NATS, so what is asserted is the configuration
// the server actually holds, not the one requested.
func TestSendingConsumerAckDeadlineOutlastsAnSMTPTransaction(t *testing.T) {
	ctx := t.Context()
	js := tests.NatsJetStream(t)
	mustSendingStream(ctx, t, js)

	s := &smtpSender{js: js}
	info, err := s.mustSendingConsumer(ctx).Info(ctx)
	require.NoError(t, err)

	assert.GreaterOrEqual(t, info.Config.AckWait, 60*time.Second,
		"an SMTP transaction cannot be acknowledged inside this deadline, so the server redelivers it and the email is sent again")
	assert.Equal(t, sendAckPolicy.FirstDeadline(), info.Config.AckWait)

	require.NotEmpty(t, info.Config.BackOff)
	assert.Equal(t, info.Config.AckWait, info.Config.BackOff[0],
		"BackOff[0] is the deadline of a first delivery: an AckWait that disagrees with it is not the deadline in force")
}

// Two deliveries of one stored Envelope are one email. This is the layer that
// holds when the deadline does not: a worker that dies mid-send, a relay stuck
// past the whole backoff curve, or any redelivery up to MaxDeliver.
func TestRedeliveredEnvelopeIsSentOnce(t *testing.T) {
	ctx := t.Context()
	js := tests.NatsJetStream(t)

	sender := &countingSender{}
	pub := &recordingPublisher{}
	s := &smtpSender{sender: sender, publisher: pub, js: js, guard: mustGetSendGuard(ctx, js)}

	msg := envelopeMsg(t, "duplicate@example.com", 42)

	require.NoError(t, s.handleMessage(ctx, msg))
	require.NoError(t, s.handleMessage(ctx, msg))

	assert.Equal(t, 1, sender.count(), "the same stored Envelope must reach the relay once")
	assert.Equal(t, []string{"kannon.stats.delivered"}, pub.subjects(),
		"a suppressed redelivery must not report a second Delivered outcome either")
}

// A Delivery that failed transiently is dispatched again by the Dispatcher as a
// new message on the stream, and that one is a genuine send: the guard keys on
// the stored message, not on the Delivery, so it must not swallow the retry.
func TestRedispatchedEnvelopeIsSentAgain(t *testing.T) {
	ctx := t.Context()
	js := tests.NatsJetStream(t)

	sender := &countingSender{}
	pub := &recordingPublisher{}
	s := &smtpSender{sender: sender, publisher: pub, js: js, guard: mustGetSendGuard(ctx, js)}

	const to = "retried@example.com"
	require.NoError(t, s.handleMessage(ctx, envelopeMsg(t, to, 1)))
	require.NoError(t, s.handleMessage(ctx, envelopeMsg(t, to, 2)))

	assert.Equal(t, 2, sender.count())
}

// End to end against a real consumer: a deadline too short for the handler —
// the shape #425 ran in production — does make the server redeliver, and the
// guard is what keeps that from reaching the relay twice.
func TestShortAckDeadlineRedeliversButDoesNotResend(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	js := tests.NatsJetStream(t)
	mustSendingStream(ctx, t, js)

	sender := &blockingSender{released: make(chan struct{})}
	pub := &recordingPublisher{}
	guard := &countingGuard{inner: mustGetSendGuard(ctx, js)}
	s := &smtpSender{sender: sender, publisher: pub, js: js, guard: guard, cfg: Config{MaxJobs: 4}}

	consumer := utils.MustGetPullSubscriber(ctx, js, "kannon-sending", "kannon.sending", "kannon-sending-pool",
		utils.WithAckPolicy(utils.AckPolicy{200 * time.Millisecond}))

	data, err := proto.Marshal(envelope("slow@example.com"))
	require.NoError(t, err)
	_, err = js.Publish(ctx, "kannon.sending", data)
	require.NoError(t, err)

	stopped := make(chan error, 1)
	go func() {
		stopped <- s.handleSend(ctx, consumer)
	}()

	// The handler is still inside the SMTP transaction, so the ack deadline
	// elapses and the server hands the same Envelope to a second worker.
	require.Eventually(t, func() bool { return guard.count() >= 2 }, 10*time.Second, 10*time.Millisecond,
		"expected the server to redeliver the Envelope while the first send was in flight")

	close(sender.released)
	assert.Eventually(t, func() bool { return len(pub.subjects()) == 1 }, 10*time.Second, 10*time.Millisecond)

	cancel()
	assert.ErrorIs(t, <-stopped, context.Canceled)

	assert.Equal(t, 1, sender.count(), "the redelivery must not reach the relay")
	assert.Equal(t, []string{"kannon.stats.delivered"}, pub.subjects())
}

func mustSendingStream(ctx context.Context, t *testing.T, js jetstream.JetStream) {
	t.Helper()
	_, err := js.CreateOrUpdateStream(ctx, jetstream.StreamConfig{
		Name:     "kannon-sending",
		Subjects: []string{"kannon.sending"},
	})
	require.NoError(t, err)
}

func envelope(to string) *msgtypes.EmailToSend {
	emailID := "<" + base64.URLEncoding.EncodeToString([]byte(to)) + "/msg_test@example.com>"
	return &msgtypes.EmailToSend{
		From:       "sender@example.com",
		To:         to,
		Body:       []byte("body"),
		EmailId:    emailID,
		ReturnPath: "bump_" + base64.URLEncoding.EncodeToString([]byte(to)) + "+msg_test@example.com",
	}
}

// envelopeMsg is one Envelope as it arrives from the stream, stored at the
// given sequence. Two messages sharing a sequence are two deliveries of the
// same stored message; different sequences are different messages.
func envelopeMsg(t *testing.T, to string, seq uint64) jetstream.Msg {
	t.Helper()
	data, err := proto.Marshal(envelope(to))
	require.NoError(t, err)
	return &fakeMsg{data: data, seq: seq}
}

// fakeMsg stands in for a JetStream message. jetstream.Msg is embedded to pick
// up the parts of the interface the handler does not touch; calling one of
// those panics loudly rather than passing silently.
type fakeMsg struct {
	jetstream.Msg
	data []byte
	seq  uint64
}

func (m *fakeMsg) Data() []byte { return m.data }

func (m *fakeMsg) Metadata() (*jetstream.MsgMetadata, error) {
	return &jetstream.MsgMetadata{
		Stream:   "kannon-sending",
		Sequence: jetstream.SequencePair{Stream: m.seq},
	}, nil
}

type countingSender struct {
	mu    sync.Mutex
	calls int
}

func (s *countingSender) SenderName() string { return "countingSender" }

func (s *countingSender) Send(_, _ string, _ []byte) smtp.SenderError {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls++
	return nil
}

func (s *countingSender) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.calls
}

// blockingSender holds the SMTP transaction open until the test releases it,
// standing in for a relay slower than the consumer's ack deadline.
type blockingSender struct {
	countingSender
	released chan struct{}
}

func (s *blockingSender) Send(from, to string, body []byte) smtp.SenderError {
	err := s.countingSender.Send(from, to, body)
	<-s.released
	return err
}

type recordingPublisher struct {
	mu   sync.Mutex
	subj []string
}

func (p *recordingPublisher) Publish(subj string, _ []byte) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.subj = append(p.subj, subj)
	return nil
}

func (p *recordingPublisher) subjects() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]string(nil), p.subj...)
}

// countingGuard counts how many deliveries reached the guard, which is how a
// test observes a redelivery that never reaches the relay.
type countingGuard struct {
	inner  sendGuard
	mu     sync.Mutex
	claims int
}

func (g *countingGuard) Claim(ctx context.Context, key string) (bool, error) {
	g.mu.Lock()
	g.claims++
	g.mu.Unlock()
	return g.inner.Claim(ctx, key)
}

func (g *countingGuard) count() int {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.claims
}
