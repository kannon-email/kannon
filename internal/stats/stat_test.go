package stats_test

import (
	"testing"
	"time"

	"github.com/kannon-email/kannon/internal/stats"
	"github.com/kannon-email/kannon/internal/tracking"
	"github.com/kannon-email/kannon/internal/values"
)

func TestEngagementTrackingModeInfersLegacyRows(t *testing.T) {
	domain := values.MustParse("example.com")
	pseudonym, err := tracking.NewPseudonym(domain.String())
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name   string
		email  string
		stored tracking.Mode
		out    stats.Outcome
		want   tracking.Mode
	}{
		{"stored mode wins", "to@example.com", tracking.ModeAnonymous, stats.Opened("ua", "1.2.3.4"), tracking.ModeAnonymous},
		{"pseudonym namespace is authoritative", pseudonym, tracking.ModeFull, stats.Opened("ua", "1.2.3.4"), tracking.ModePseudonymous},
		{"legacy request detail proves full", "to@example.com", "", stats.Clicked("ua", "", "https://x"), tracking.ModeFull},
		{"legacy without detail is identified", "to@example.com", "", stats.Opened("", ""), tracking.ModeIdentified},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s := stats.NewStat(c.email, "msg", domain, time.Now(), c.out)
			s.TrackingMode = c.stored
			if got := s.EngagementTrackingMode(); got != c.want {
				t.Fatalf("got %q, want %q", got, c.want)
			}
		})
	}
}
