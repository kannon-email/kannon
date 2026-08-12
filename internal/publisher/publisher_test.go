package publisher

import (
	"testing"

	"github.com/kannon-email/kannon/internal/envelope"
	"github.com/kannon-email/kannon/internal/envelopepb"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSendEmailPublishesOnTheSendingSubject pins what this package still owns
// now that the field mapping has moved to internal/envelopepb, which is tested
// there: the subject the SMTPSender's consumer filters on, and the fact that
// what lands on it is the marshalled Envelope. A message published anywhere
// else is a batch that never leaves.
//
// The payload is read back through envelopepb rather than the generated type,
// which is what the SMTPSender does with it, so this asserts the pair actually
// in use rather than one end against a hand-written decode.
func TestSendEmailPublishesOnTheSendingSubject(t *testing.T) {
	p := &recordingPublisher{}

	require.NoError(t, SendEmail(p, envelope.New(envelope.Params{
		EmailID:    "id",
		From:       "f@x",
		To:         "t@x",
		ReturnPath: "rp",
		Body:       []byte("body"),
	})))

	require.Equal(t, []string{"kannon.sending"}, p.subjects)
	require.Len(t, p.payloads, 1)

	got, err := envelopepb.UnmarshalEnvelope(p.payloads[0])
	require.NoError(t, err)
	assert.Equal(t, "id", got.EmailID())
	assert.Equal(t, "t@x", got.To())
	assert.Equal(t, []byte("body"), got.Body())
}

type recordingPublisher struct {
	subjects []string
	payloads [][]byte
}

func (p *recordingPublisher) Publish(subj string, data []byte) error {
	p.subjects = append(p.subjects, subj)
	p.payloads = append(p.payloads, data)
	return nil
}
