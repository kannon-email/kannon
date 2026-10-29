package stats

import (
	"context"
	"errors"
	"time"

	"github.com/kannon-email/kannon/internal/values"
)

// StoredEventID names a stored stream message, independently of its payload and
// consumer delivery attempt. StreamCreated distinguishes recreated streams whose
// sequences start over. It must be the server's stream creation time, not local time.
// No Recipient identity is retained here, including for Anonymous events.
type StoredEventID struct {
	Stream        string
	StreamCreated time.Time
	Sequence      uint64
}

// ErrInvalidStoredEventID prevents counting without a stable delivery identity.
var ErrInvalidStoredEventID = errors.New("invalid stored stat event identity")

// Validate checks that the transport supplied a complete stored-message identity.
func (id StoredEventID) Validate() error {
	if id.Stream == "" || id.StreamCreated.IsZero() || id.Sequence == 0 {
		return ErrInvalidStoredEventID
	}
	return nil
}

// AggregatedEventCounter atomically records a stored event and increments its
// Domain's UTC hourly counter once. A duplicate succeeds without incrementing;
// a failed write leaves the event retryable. Distinct stored events always count
// independently, even if their complete payloads match. Implementations retain
// markers for the entire replay window (currently unbounded for kannon-stats).
// This is a trusted worker write, with the same authority as InsertStat.
type AggregatedEventCounter interface {
	Count(ctx context.Context, id StoredEventID, domain values.DomainName, timestamp time.Time, statType Type) error
}
