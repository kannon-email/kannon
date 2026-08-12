package envelopepb_test

import (
	"testing"

	"github.com/kannon-email/kannon/internal/envelope"
	"github.com/kannon-email/kannon/internal/envelopepb"
	pb "github.com/kannon-email/kannon/proto/kannon/mailer/types"
	"github.com/stretchr/testify/assert"
)

// oneEnvelope is a fully-stated Envelope: every field holds a distinct non-zero
// value, so a mapping that crossed two of them over could not pass any of the
// tests below.
func oneEnvelope() *envelope.Envelope {
	return envelope.New(envelope.Params{
		EmailID:     "id",
		From:        "f@x",
		To:          "t@x",
		ReturnPath:  "rp",
		Body:        []byte("body"),
		ShouldRetry: true,
	})
}

// TestFromEnvelope pins the field mapping SendEmail puts on kannon.sending. It
// is asserted literally rather than only round-tripped, because a pair of
// translations that agreed with each other and with nothing else would still
// leave a Dispatcher and an SMTPSender on different builds — the normal state
// of a rollout — reading and writing different messages.
func TestFromEnvelope(t *testing.T) {
	msg := envelopepb.FromEnvelope(oneEnvelope())

	assert.Equal(t, "id", msg.EmailId)
	assert.Equal(t, "f@x", msg.From)
	assert.Equal(t, "t@x", msg.To)
	assert.Equal(t, "rp", msg.ReturnPath)
	assert.Equal(t, []byte("body"), msg.Body)
	assert.True(t, msg.ShouldRetry)
}

// TestToEnvelope pins the read side, which is the one that decides where a real
// email goes: the SMTPSender hands To and ReturnPath straight to a relay, and
// takes the Delivery it reports on from the email ID.
func TestToEnvelope(t *testing.T) {
	env := envelopepb.ToEnvelope(&pb.EmailToSend{
		EmailId:     "id",
		From:        "f@x",
		To:          "t@x",
		ReturnPath:  "rp",
		Body:        []byte("body"),
		ShouldRetry: true,
	})

	assert.Equal(t, "id", env.EmailID())
	assert.Equal(t, "f@x", env.From())
	assert.Equal(t, "t@x", env.To())
	assert.Equal(t, "rp", env.ReturnPath())
	assert.Equal(t, []byte("body"), env.Body())
	assert.True(t, env.ShouldRetry())
}

// TestEnvelopeRoundTrip pins that the Envelope the SMTPSender transmits is the
// Envelope the Dispatcher built. The two directions are one mapping stated
// once, so a field added to the entity and forgotten on either side shows up
// here as a value that did not survive the trip.
func TestEnvelopeRoundTrip(t *testing.T) {
	env := oneEnvelope()
	assert.Equal(t, env, envelopepb.ToEnvelope(envelopepb.FromEnvelope(env)))
}

// TestToEnvelopeOfNil covers the total reading: a message that states nothing
// is an Envelope that states nothing, and never a nil the caller has to check
// for before reaching a getter.
func TestToEnvelopeOfNil(t *testing.T) {
	assert.Equal(t, envelope.New(envelope.Params{}), envelopepb.ToEnvelope(nil))
}
