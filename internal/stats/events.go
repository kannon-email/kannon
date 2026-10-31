package stats

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"github.com/kannon-email/kannon/internal/apperr"
	"slices"
	"sort"
	"time"

	"github.com/kannon-email/kannon/internal/authz"
	"github.com/kannon-email/kannon/internal/values"
)

var ErrEventQuery = apperr.New(apperr.Malformed, "invalid_query_parameter", "invalid event query")
var ErrEventCursor = apperr.New(apperr.Malformed, "invalid_cursor", "invalid event cursor")

// EventQuery selects retained public outcomes with a half-open interval.
type EventQuery struct {
	Range   TimeRange
	Types   []Type
	BatchID string
	Limit   int
	Cursor  string
}
type EventPage struct {
	Items      []*Stat
	NextCursor string
}
type eventCursor struct {
	Context   string
	Timestamp time.Time
	ID        int32
}

// QueryEvents enumerates retained outcomes without consulting live history.
func (s *Service) QueryEvents(ctx context.Context, d values.DomainName, q EventQuery) (EventPage, error) {
	return authz.Guard(ctx, authz.List, authz.Stats(d), func() (EventPage, error) {
		if q.Limit < 1 || q.Limit > 1000 || q.Range.Start.IsZero() || !q.Range.Stop.After(q.Range.Start) {
			return EventPage{}, ErrEventQuery
		}
		q.Types = slices.Clone(q.Types)
		sort.Slice(q.Types, func(i, j int) bool { return q.Types[i] < q.Types[j] })
		q.Types = slices.Compact(q.Types)
		for _, t := range q.Types {
			if !publicType(t) {
				return EventPage{}, ErrEventQuery
			}
		}
		b, err := json.Marshal([]any{d.String(), q.Range.Start.UTC(), q.Range.Stop.UTC(), q.Types, q.BatchID})
		if err != nil {
			return EventPage{}, err
		}
		hash := sha256.Sum256(b)
		binding := base64.RawURLEncoding.EncodeToString(hash[:])
		var after eventCursor
		if q.Cursor != "" {
			raw, err := base64.RawURLEncoding.DecodeString(q.Cursor)
			if err != nil || json.Unmarshal(raw, &after) != nil || after.Context != binding || after.ID <= 0 || after.Timestamp.IsZero() {
				return EventPage{}, ErrEventCursor
			}
		}
		rows, err := s.repo.QueryEvents(ctx, d, q, after.Timestamp, after.ID)
		if err != nil {
			return EventPage{}, err
		}
		page := EventPage{Items: rows}
		if len(rows) > q.Limit {
			page.Items = rows[:q.Limit]
			last := page.Items[len(page.Items)-1]
			raw, err := json.Marshal(eventCursor{binding, last.Timestamp, last.ID})
			if err != nil {
				return EventPage{}, err
			}
			page.NextCursor = base64.RawURLEncoding.EncodeToString(raw)
		}
		return page, nil
	})
}
func publicType(t Type) bool {
	return slices.Contains([]Type{TypeAccepted, TypeRejected, TypeDelivered, TypeBounce, TypeFailed, TypeOpened, TypeClicked}, t)
}
