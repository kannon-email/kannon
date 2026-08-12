package stats

import (
	"time"

	"github.com/kannon-email/kannon/internal/tracking"
)

// Event is one Outcome as it travels between Kannon's workers on the
// kannon.stats.* topics: what happened, to whose Delivery, and when.
//
// It is not a Stat. A Stat is the row the Stats worker leaves behind, and it
// names its Domain with a values.DomainName because a row filed under a
// spelling no query uses is invisible to the tenant it belongs to. An Event
// names its Domain with a plain string because that is the state the value is
// in when a producer has it — sliced out of an email id, or out of a bounce
// return path — and because the consumer, not the producer, is the one placed
// to decide what to do when it turns out not to be a domain name at all:
// pkg/stats.eventDomain canonicalises it and Terms the message when it cannot,
// since Nak'ing would reproduce the #396 hot loop.
//
// TrackingMode is the Mode that governed the engagement channel the event came
// from, so a consumer can tell an Opened with no ip / user_agent because the
// Mode forbade retaining them from one that merely lacks them. Only engagement
// events state one; every other outcome leaves it unspecified.
type Event struct {
	MessageID    string
	Domain       string
	Email        string
	Timestamp    time.Time
	Outcome      Outcome
	TrackingMode tracking.Mode
}
