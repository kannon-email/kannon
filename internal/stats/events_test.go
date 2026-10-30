package stats_test

import (
	"errors"
	"testing"
	"time"

	"github.com/kannon-email/kannon/internal/stats"
)

func TestEventPagesBindFiltersAndExcludeTransientSignals(t *testing.T) {
	s := newTestService()
	ctx := authorizedCtx(t)
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	for _, outcome := range []stats.Outcome{stats.Accepted(), stats.Delivered(), stats.Errored(451, "retry")} {
		if err := s.InsertStat(ctx, stats.NewStat("a@example.com", "batch", exampleCom, now, outcome)); err != nil {
			t.Fatal(err)
		}
	}
	q := stats.EventQuery{Range: stats.TimeRange{Start: now, Stop: now.Add(time.Hour)}, Limit: 1}
	page, err := s.QueryEvents(ctx, exampleCom, q)
	if err != nil || len(page.Items) != 1 || page.Items[0].Type != stats.TypeDelivered || page.NextCursor == "" {
		t.Fatal(page, err)
	}
	q.Cursor = page.NextCursor
	page, err = s.QueryEvents(ctx, exampleCom, q)
	if err != nil || len(page.Items) != 1 || page.Items[0].Type != stats.TypeAccepted || page.NextCursor != "" {
		t.Fatal(page, err)
	}
	q.BatchID = "other"
	if _, err := s.QueryEvents(ctx, exampleCom, q); !errors.Is(err, stats.ErrEventCursor) {
		t.Fatal(err)
	}
}
