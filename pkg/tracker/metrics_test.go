package tracker

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kannon-email/kannon/internal/mailflow"
	"github.com/kannon-email/kannon/internal/statssec"
	"github.com/kannon-email/kannon/internal/tracking"
)

// An Anonymous event is published, so it is counted like any other; a refused token publishes
// nothing and counts nothing.
func testEngagementIsCountedByKind(t *testing.T, ss statssec.StatsService) {
	reg := prometheus.NewRegistry()
	srv := newServer(&fakePublisher{}, ss, mailflow.New(reg))
	get := func(path string) {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.Host = "stats." + testDomain
		srv.handler().ServeHTTP(httptest.NewRecorder(), req)
	}

	openToken, err := ss.CreateOpenToken(t.Context(), testMessageID, testRecipient, tracking.ModeAnonymous)
	require.NoError(t, err)
	linkToken, err := ss.CreateLinkToken(t.Context(), testMessageID, testRecipient, testLandingURL, tracking.ModeIdentified)
	require.NoError(t, err)

	get("/o/" + openToken)
	get("/c/" + linkToken)
	get("/c/" + linkToken)
	get("/o/not-a-token")

	want := `
# HELP kannon_engagement_events_total Engagement events published by the Tracker, by kind.
# TYPE kannon_engagement_events_total counter
kannon_engagement_events_total{kind="clicked"} 2
kannon_engagement_events_total{kind="opened"} 1
`
	assert.NoError(t, testutil.GatherAndCompare(reg, strings.NewReader(want), "kannon_engagement_events_total"))
}
