package smtpsender

import (
	"context"
	"encoding/base64"
	"sync"
	"testing"
	"time"

	"github.com/kannon-email/kannon/internal/envelope"
	"github.com/kannon-email/kannon/internal/envelopepb"
	"github.com/kannon-email/kannon/internal/smtp"
	"github.com/kannon-email/kannon/internal/tests"
	"github.com/kannon-email/kannon/internal/utils"
	statstypes "github.com/kannon-email/kannon/proto/kannon/stats/types"
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

	_, err := js.Publish(ctx, "kannon.sending", envelopeBytes(t, "slow@example.com"))
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

// TestHandleSendErrorClassifiesByReplyClassNotByRetryDecision is the
// synchronous mirror of the asynchronous DSN assertion in
// e2e/e2e_test.go:testAsyncSoftBounce (~line 711): Bounced.Permanent must
// track the SMTP reply class, never the reason handleSendError took the
// Bounced branch in the first place.
//
// #378 read the line `Permanent: sendErr.IsPermanent()` at retry exhaustion
// as the wrong flag — "it should be true, we gave up". #433 re-specified
// `permanent` to follow the reply class on both the synchronous and the
// asynchronous path (CONTEXT.md, Bounced), which is exactly what the code
// already did: a transient (4xx) reply stays non-permanent even once
// ShouldRetry is false, and a permanent (5xx) reply is permanent regardless
// of ShouldRetry. This test pins that mapping down so it cannot be "fixed"
// into a regression again.
func TestHandleSendErrorClassifiesByReplyClassNotByRetryDecision(t *testing.T) {
	tests := []struct {
		name          string
		shouldRetry   bool
		sendErr       *fakeSenderError
		wantSubject   string
		wantPermanent bool
	}{
		{
			name:          "retries exhausted, transient (4xx) reply publishes Bounced with permanent=false",
			shouldRetry:   false,
			sendErr:       &fakeSenderError{msg: "451 try again later", permanent: false, code: 451},
			wantSubject:   "kannon.stats.bounced",
			wantPermanent: false,
		},
		{
			name:          "retries exhausted, permanent (5xx) reply publishes Bounced with permanent=true",
			shouldRetry:   false,
			sendErr:       &fakeSenderError{msg: "550 no such user", permanent: true, code: 550},
			wantSubject:   "kannon.stats.bounced",
			wantPermanent: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pub := &recordingPublisher{}
			s := &smtpSender{publisher: pub}

			require.NoError(t, s.handleSendError(tt.sendErr, envelopeTo("bounce@example.com", tt.shouldRetry)))

			require.Equal(t, []string{tt.wantSubject}, pub.subjects())
			published := pub.stats(t)
			require.Len(t, published, 1)

			bounced := published[0].Data.GetBounced()
			require.NotNil(t, bounced, "expected a typed Bounced payload")
			assert.Equal(t, tt.wantPermanent, bounced.Permanent)
			assert.EqualValues(t, tt.sendErr.code, bounced.Code)
			assert.Equal(t, tt.sendErr.msg, bounced.Msg)
		})
	}
}

// TestHandleSendErrorWithRetriesRemainingPublishesError contrasts the
// exhausted-retries cases above: a transient reply with retries still
// available must publish the internal Error retry signal, not a Bounced
// outcome — Bounced is reserved for a Delivery that is actually terminal.
func TestHandleSendErrorWithRetriesRemainingPublishesError(t *testing.T) {
	pub := &recordingPublisher{}
	s := &smtpSender{publisher: pub}

	sendErr := &fakeSenderError{msg: "451 try again later", permanent: false, code: 451}

	require.NoError(t, s.handleSendError(sendErr, envelopeTo("retry@example.com", true)))

	require.Equal(t, []string{"kannon.stats.error"}, pub.subjects())
	published := pub.stats(t)
	require.Len(t, published, 1)

	errData := published[0].Data.GetError()
	require.NotNil(t, errData, "expected a typed Error payload")
	assert.EqualValues(t, sendErr.code, errData.Code)
	assert.Equal(t, sendErr.msg, errData.Msg)
	assert.Nil(t, published[0].Data.GetBounced(), "a retryable transient failure must not be reported as Bounced")
}

// fakeSenderError is a local stand-in for smtp.SenderError: the concrete
// smtpError type in internal/smtp is unexported, so a test outside that
// package cannot construct one directly.
type fakeSenderError struct {
	msg       string
	permanent bool
	code      uint32
}

func (e *fakeSenderError) Error() string     { return e.msg }
func (e *fakeSenderError) IsPermanent() bool { return e.permanent }
func (e *fakeSenderError) Code() uint32      { return e.code }

func mustSendingStream(ctx context.Context, t *testing.T, js jetstream.JetStream) {
	t.Helper()
	_, err := js.CreateOrUpdateStream(ctx, jetstream.StreamConfig{
		Name:     "kannon-sending",
		Subjects: []string{"kannon.sending"},
	})
	require.NoError(t, err)
}

// envelopeTo is one built Envelope, as the Dispatcher hands it over.
func envelopeTo(to string, shouldRetry bool) *envelope.Envelope {
	emailID := "<" + base64.URLEncoding.EncodeToString([]byte(to)) + "/msg_test@example.com>"
	return envelope.New(envelope.Params{
		From:        "sender@example.com",
		To:          to,
		Body:        []byte("body"),
		EmailID:     emailID,
		ReturnPath:  "bump_" + base64.URLEncoding.EncodeToString([]byte(to)) + "+msg_test@example.com",
		ShouldRetry: shouldRetry,
	})
}

// envelopeBytes is an Envelope as it sits on the sending stream: published
// through the same translation the Dispatcher uses, so a test drives the
// handler over the real payload rather than one assembled by hand.
func envelopeBytes(t *testing.T, to string) []byte {
	t.Helper()
	data, err := proto.Marshal(envelopepb.FromEnvelope(envelopeTo(to, false)))
	require.NoError(t, err)
	return data
}

// envelopeMsg is one Envelope as it arrives from the stream, stored at the
// given sequence. Two messages sharing a sequence are two deliveries of the
// same stored message; different sequences are different messages.
func envelopeMsg(t *testing.T, to string, seq uint64) jetstream.Msg {
	t.Helper()
	return &fakeMsg{data: envelopeBytes(t, to), seq: seq}
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
	data [][]byte
}

func (p *recordingPublisher) Publish(subj string, data []byte) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.subj = append(p.subj, subj)
	p.data = append(p.data, data)
	return nil
}

func (p *recordingPublisher) subjects() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]string(nil), p.subj...)
}

// stats unmarshals every payload published so far, in publish order, so a
// test can assert on the typed StatsData a subject carried rather than only
// on the subject it was published to.
func (p *recordingPublisher) stats(t *testing.T) []*statstypes.Stats {
	t.Helper()
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]*statstypes.Stats, len(p.data))
	for i, d := range p.data {
		s := &statstypes.Stats{}
		require.NoError(t, proto.Unmarshal(d, s))
		out[i] = s
	}
	return out
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
