package intake

import (
	"encoding/base64"
	"fmt"
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

// MaxFieldsPerRecipient bounds the fields one Recipient carries, its global fields included.
const MaxFieldsPerRecipient = 1000

// MaxFieldsBytes bounds the fields of one Batch, keys and values plus FieldOverheadBytes for
// each field, summed over its Recipients with the global fields counted once for each. It matches
// the REST body limit, so a Batch never stores more fields than one request could carry without
// global fields.
const MaxFieldsBytes = 10 << 20

// FieldOverheadBytes is what one field is charged beyond its key and value: copying it costs a
// map entry and stores its JSON punctuation however short it is, so empty fields are not free.
const FieldOverheadBytes = 64

// ErrTooManyFields and ErrFieldsTooLarge are the limits a FieldsLimitError reports.
var (
	ErrTooManyFields = apperr.New(apperr.Invalid, "too_many_fields",
		fmt.Sprintf("A Recipient may carry at most %d fields, global fields included", MaxFieldsPerRecipient))
	ErrFieldsTooLarge = apperr.New(apperr.Invalid, "fields_too_large",
		fmt.Sprintf("The fields of a Batch, with %d bytes charged for each, may total at most %d MiB, global fields counted once per Recipient",
			FieldOverheadBytes, MaxFieldsBytes>>20))
)

// GlobalFields stands for a Batch's global fields where a FieldsLimitError names a Recipient.
const GlobalFields = -1

// FieldsLimitError refuses a Batch whose fields exceed ErrTooManyFields or ErrFieldsTooLarge,
// and names the fields to shrink: those of Recipient, an index into the request, or GlobalFields.
type FieldsLimitError struct {
	Limit     *apperr.Error
	Recipient int
}

func (e *FieldsLimitError) Error() string { return e.Limit.Error() }

func (e *FieldsLimitError) Unwrap() error { return e.Limit }

// checkFieldsBudget refuses a Batch whose fields would be expensive to copy, before any copy is
// made: global fields are merged into every Recipient and stored with each Delivery, so a request
// that is small on the wire multiplies by the number of Recipients. A global field a Recipient
// overrides is counted twice, which keeps the measure an upper bound computed without merging.
//
// Global fields are blamed for a refusal whenever they take part in it, since shrinking them is
// what shrinks every Recipient.
func checkFieldsBudget(p InlineRequest) error {
	if len(p.GlobalFields) > MaxFieldsPerRecipient {
		return &FieldsLimitError{Limit: ErrTooManyFields, Recipient: GlobalFields}
	}
	global := fieldsSize(p.GlobalFields)
	total := 0
	for i, r := range p.Recipients {
		if len(p.GlobalFields)+len(r.Fields) > MaxFieldsPerRecipient {
			return &FieldsLimitError{Limit: ErrTooManyFields, Recipient: i}
		}
		total += global + fieldsSize(r.Fields)
		if total > MaxFieldsBytes {
			if len(p.GlobalFields) > 0 {
				return &FieldsLimitError{Limit: ErrFieldsTooLarge, Recipient: GlobalFields}
			}
			return &FieldsLimitError{Limit: ErrFieldsTooLarge, Recipient: i}
		}
	}
	return nil
}

func fieldsSize(fields map[string]string) int {
	size := 0
	for k, v := range fields {
		size += len(k) + len(v) + FieldOverheadBytes
	}
	return size
}
