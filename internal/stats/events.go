package stats

import (
	"context"
	"errors"
	"slices"
	"sort"
	"time"

	"github.com/kannon-email/kannon/internal/authz"
	"github.com/kannon-email/kannon/internal/keyset"
	"github.com/kannon-email/kannon/internal/values"
)

var ErrEventQuery = errors.New("invalid event query")

// EventQuery selects retained public outcomes with a half-open interval.
type EventQuery struct {
	Range   TimeRange
	Types   []Type
	BatchID string
}

// EventPosition is an event's place in the newest-first event order: timestamp, then ID.
type EventPosition struct {
	Timestamp time.Time
	ID        int32
}

// EventPositionOf reports where s sits in the newest-first event order.
func EventPositionOf(s *Stat) EventPosition {
	return EventPosition{Timestamp: s.Timestamp, ID: s.ID}
}

// QueryEvents enumerates retained outcomes newest first, without consulting live history.
func (s *Service) QueryEvents(ctx context.Context, d values.DomainName, q EventQuery, page keyset.Page[EventPosition]) ([]*Stat, error) {
	return authz.Guard(ctx, authz.List, authz.Stats(d), func() ([]*Stat, error) {
		if page.Limit < 1 || q.Range.Start.IsZero() || !q.Range.Stop.After(q.Range.Start) {
			return nil, ErrEventQuery
		}
		q.Types = slices.Clone(q.Types)
		sort.Slice(q.Types, func(i, j int) bool { return q.Types[i] < q.Types[j] })
		q.Types = slices.Compact(q.Types)
		for _, t := range q.Types {
			if !publicType(t) {
				return nil, ErrEventQuery
			}
		}
		return s.repo.QueryEvents(ctx, d, q, page)
	})
}
func publicType(t Type) bool {
	return slices.Contains([]Type{TypeAccepted, TypeRejected, TypeDelivered, TypeBounce, TypeFailed, TypeOpened, TypeClicked}, t)
}
