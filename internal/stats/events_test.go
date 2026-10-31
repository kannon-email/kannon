package stats_test

import (
	"testing"
	"time"

	"github.com/kannon-email/kannon/internal/keyset"
	"github.com/kannon-email/kannon/internal/stats"
)

func TestEventPagesResumeAfterPositionAndExcludeTransientSignals(t *testing.T) {
	s := newTestService()
	ctx := authorizedCtx(t)
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	for _, outcome := range []stats.Outcome{stats.Accepted(), stats.Delivered(), stats.Errored(451, "retry")} {
		if err := s.InsertStat(ctx, stats.NewStat("a@example.com", "batch", exampleCom, now, outcome)); err != nil {
			t.Fatal(err)
		}
	}
	q := stats.EventQuery{Range: stats.TimeRange{Start: now, Stop: now.Add(time.Hour)}}
	page := keyset.Page[stats.EventPosition]{Limit: 1}
	rows, err := s.QueryEvents(ctx, exampleCom, q, page)
	if err != nil || len(rows) != 1 || rows[0].Type != stats.TypeDelivered {
		t.Fatal(rows, err)
	}
	after := stats.EventPositionOf(rows[0])
	page.After = &after
	rows, err = s.QueryEvents(ctx, exampleCom, q, page)
	if err != nil || len(rows) != 1 || rows[0].Type != stats.TypeAccepted {
		t.Fatal(rows, err)
	}
	after = stats.EventPositionOf(rows[0])
	rows, err = s.QueryEvents(ctx, exampleCom, q, page)
	if err != nil || len(rows) != 0 {
		t.Fatal(rows, err)
	}
}
