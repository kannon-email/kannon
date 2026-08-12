package batch

import (
	"strings"

	"github.com/kannon-email/kannon/internal/tracking"
)

// Recipient is the input description of one target for a Batch (CONTEXT.md): an
// address, the fields that personalise the message sent to it, and the Tracking
// Policy stated at the most specific level of the cascade (ADR 0003). It is input
// data and has no row of its own — accepted, it becomes a Delivery; Rejected, it is
// reported to the caller and nothing is created for it.
//
// It lives here, beside Sender, Headers and OneClickUnsubscribe, because those are
// the things one send states about itself and this is the last of them. It is a
// Kannon type rather than the generated mailertypes.Recipient because what makes a
// Recipient acceptable is a business rule, while the shape of the wire message is a
// compatibility contract with the clients built against it (ADR 0012): a rule about
// Recipients should be answerable without constructing a protobuf message to ask.
type Recipient struct {
	Email  string
	Fields map[string]string
	// Tracking is the Policy this Recipient states for itself, which may only
	// narrow what its Batch and Domain allow. Zero when it states nothing, which
	// imposes no restriction of its own.
	Tracking tracking.Policy
}

// HasAddress reports whether the Recipient names an address at all. Whitespace is
// not an address: a row holding only spaces is an empty cell in whatever list the
// caller exported, and Kannon has nowhere to deliver it.
//
// This is the whole of what a Recipient can be judged on by itself. The other
// grounds it may be Rejected on — a Policy above its Domain's ceiling, an
// unsubscribe URL its fields leave unresolved — are questions about a Recipient
// against something else, and are asked at intake where both are in hand.
func (r Recipient) HasAddress() bool {
	return strings.TrimSpace(r.Email) != ""
}
