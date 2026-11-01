package intake

import (
	"encoding/base64"
	"math"

	"github.com/kannon-email/kannon/internal/apperr"
	"github.com/kannon-email/kannon/internal/batch"
	"github.com/kannon-email/kannon/internal/utils"
)

// ErrMessageTooLarge refuses a Batch whose content exceeds the configured payload.
var ErrMessageTooLarge = apperr.New(apperr.Invalid, "message_too_large", "")

// checkMessageSize refuses content larger than maxPayload, so the caller is told now rather than
// the Delivery failing in the Dispatcher on every attempt until its Retry Budget runs out: each
// built Envelope travels as one NATS message, which the server refuses above its max_payload.
// Zero leaves the size unchecked.
//
// The measure is the text — the subject and the HTML, after whatever field substitution has
// already been applied — plus each attachment as base64, the form it takes in the message.
// Building adds the other headers, the DKIM signature and tracking links on top, which is why
// maxPayload is configured below the NATS limit rather than at it.
func checkMessageSize(maxPayload, text int, attachments batch.Attachments) error {
	if text > textRoom(maxPayload, attachments) {
		return ErrMessageTooLarge
	}
	return nil
}

// textRoom is how many bytes of text one message may carry beside its attachments: unbounded
// when maxPayload is zero, negative when the attachments alone exceed it.
//
// Personalised text is measured against it per Recipient, not only per Batch, because field
// substitution multiplies: a placeholder repeated a hundred thousand times in a body that fits,
// against a value of fifty kilobytes, renders to five gigabytes. Measuring after rendering would
// already have paid for it, so every check against this room is made before the text is built.
func textRoom(maxPayload int, attachments batch.Attachments) int {
	if maxPayload <= 0 {
		return math.MaxInt
	}
	size := 0
	for _, content := range attachments {
		size += base64.StdEncoding.EncodedLen(len(content))
	}
	return maxPayload - size
}

// unsubscribeURLFits reports whether a Recipient's unsubscribe URL renders within room. The URL
// is a header written per Delivery from a template the caller states, so it is personalised text
// like the subject and the body, and amplifies the same way.
func unsubscribeURLFits(u batch.OneClickUnsubscribe, fields map[string]string, room int) bool {
	if u.IsZero() {
		return room >= 0
	}
	_, ok := utils.ReplaceCustomFieldsInURLWithin(u.URLTemplate, fields, room)
	return ok
}
