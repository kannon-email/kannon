package intake

import (
	"encoding/base64"

	"github.com/kannon-email/kannon/internal/apperr"
	"github.com/kannon-email/kannon/internal/batch"
)

// ErrMessageTooLarge refuses a Batch whose content could not travel as an Envelope.
var ErrMessageTooLarge = apperr.New(apperr.Invalid, "message_too_large", "")

// The share of the largest Envelope the transport carries that a Batch's content may fill.
// The Dispatcher publishes each built Envelope as one NATS message, and building adds to the
// content measured here: headers, the DKIM signature, tracking links rewritten into the HTML,
// quoted-printable escapes and the base64 line breaks. The remaining 30% is that headroom.
const contentShareOfEnvelope = 0.7

// checkMessageSize refuses content that would build into an Envelope the transport cannot carry,
// so the caller is told now rather than the Delivery failing in the Dispatcher on every attempt
// until its Retry Budget runs out. envelopeMaxBytes is the largest Envelope the transport carries;
// zero leaves the size unchecked.
//
// The measure is the HTML plus each attachment as base64, the form it takes in the message. A
// Recipient's fields can grow the HTML further; the headroom absorbs that too.
func checkMessageSize(envelopeMaxBytes int, html string, attachments batch.Attachments) error {
	if envelopeMaxBytes <= 0 {
		return nil
	}
	size := len(html)
	for _, content := range attachments {
		size += base64.StdEncoding.EncodedLen(len(content))
	}
	if float64(size) > float64(envelopeMaxBytes)*contentShareOfEnvelope {
		return ErrMessageTooLarge
	}
	return nil
}
