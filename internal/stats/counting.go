package stats

import (
	"context"
	"errors"
	"time"

	"github.com/kannon-email/kannon/internal/values"
)

// StoredEventID names a stored stream message, independently of payload and
// delivery attempt. StoredAt is the original server storage timestamp from the
// message metadata, not the event timestamp or a worker's stream snapshot.
// Together with the stream and sequence it distinguishes sequence reuse after
// recreation without racing a separate lookup of stream creation time.
// No Recipient identity is retained, including for Anonymous events.
type StoredEventID struct {
	Stream   string
	StoredAt time.Time
	Sequence uint64
}

// CountedEventRetention is how long a counting marker is kept after the event it names was stored:
// the stats stream's max age, after which the event can no longer be redelivered, plus a day. The
// margin covers the stored time being stamped by the NATS server's clock and the cutoff computed on
// the worker's, so a skew between them cannot drop a marker while its event is still redeliverable.
const CountedEventRetention = StreamMaxAge + 24*time.Hour

// ErrInvalidStoredEventID prevents counting without a stable delivery identity.
var ErrInvalidStoredEventID = errors.New("invalid stored stat event identity")

// Validate checks that the transport supplied a complete stored-message identity.
func (id StoredEventID) Validate() error {
	if id.Stream == "" || id.StoredAt.IsZero() || id.Sequence == 0 {
		return ErrInvalidStoredEventID
	}
	return nil
}

// AggregatedEventCounter atomically records a stored event and increments its
// Domain's UTC hourly counter once. A duplicate succeeds without incrementing;
// a failed write leaves the event retryable. Distinct stored events always count
// independently, even if their complete payloads match. Implementations retain
// markers for CountedEventRetention, which outlasts the stream's replay window.
// This is a trusted worker write, with the same authority as InsertStat.
type AggregatedEventCounter interface {
	Count(ctx context.Context, id StoredEventID, domain values.DomainName, timestamp time.Time, statType Type) error
}
