package intake

import (
	"encoding/base64"

	"github.com/kannon-email/kannon/internal/apperr"
	"github.com/kannon-email/kannon/internal/batch"
)

// ErrMessageTooLarge refuses a Batch whose content exceeds the configured payload.
var ErrMessageTooLarge = apperr.New(apperr.Invalid, "message_too_large", "")

// checkMessageSize refuses content larger than maxPayload, so the caller is told now rather than
// the Delivery failing in the Dispatcher on every attempt until its Retry Budget runs out: each
// built Envelope travels as one NATS message, which the server refuses above its max_payload.
// Zero leaves the size unchecked.
//
// The measure is the HTML plus each attachment as base64, the form it takes in the message.
// Building adds headers, the DKIM signature and tracking links on top, which is why maxPayload
// is configured below the NATS limit rather than at it.
func checkMessageSize(maxPayload int, html string, attachments batch.Attachments) error {
	if maxPayload <= 0 {
		return nil
	}
	size := len(html)
	for _, content := range attachments {
		size += base64.StdEncoding.EncodedLen(len(content))
	}
	if size > maxPayload {
		return ErrMessageTooLarge
	}
	return nil
}
