package e2e_test

import (
	"fmt"
	"net/http"
	"regexp"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kannon-email/kannon/e2e/restclient"
	"github.com/kannon-email/kannon/internal/tests"
)

var trackingScenarios = map[string]func(*testing.T, *env){
	"Opened":            testOpened,
	"Clicked":           testClicked,
	"Off":               testTrackingOff,
	"Identified":        testTrackingIdentified,
	"Full":              testTrackingFull,
	"Anonymous":         testTrackingAnonymous,
	"MixedPerRecipient": testMixedRecipientTracking,
}

const landingURL = "https://example.com/landing"

var trackedHTML = fmt.Sprintf(`<html><body><h1>Hello!</h1><a href=%q>click</a></body></html>`, landingURL)

// The tracking pixel and the tracked link, as the Envelope builder writes them into the body.
var (
	pixelPath = regexp.MustCompile(`/o/[A-Za-z0-9._-]+`)
	linkPath  = regexp.MustCompile(`/c/[A-Za-z0-9._-]+`)
)

func trackingPath(t *testing.T, re *regexp.Regexp, body string) string {
	t.Helper()
	p := re.FindString(body)
	require.NotEmpty(t, p, "%s not found in body: %q", re, body)
	return p
}

// engage sends one tracked message, opens it and clicks its link the way the Recipient's client
// would, and returns the resulting Opened and Clicked events.
func engage(t *testing.T, e *env, tn *tenant, subject string) (opened, clicked event) {
	t.Helper()
	rcpt := tests.FakeEmail(t)
	tn.Send(t, tn.Batch(subject, trackedHTML, To(rcpt, nil)))
	msg := e.MX.AwaitEnvelope(t, rcpt)

	status, _ := e.Track(t, tn.StatsHost(), trackingPath(t, pixelPath, msg.Body))
	assert.Equal(t, http.StatusOK, status)
	status, location := e.Track(t, tn.StatsHost(), trackingPath(t, linkPath, msg.Body))
	assert.Equal(t, http.StatusTemporaryRedirect, status)
	assert.Equal(t, landingURL, location, "the click redirects to the originally-authored URL")

	return tn.Await(t, rcpt, Opened), tn.Await(t, rcpt, Clicked)
}

// testOpened: a fresh Domain resolves to Identified (ADR 0003), so the open is attributed to the
// Recipient and nothing about the request itself is retained.
func testOpened(t *testing.T, e *env) {
	tn := e.NewTenant(t)
	rcpt := tests.FakeEmail(t)

	tn.Send(t, tn.Batch("Opened Test", "<html><body><h1>Hello!</h1></body></html>", To(rcpt, nil)))
	msg := e.MX.AwaitEnvelope(t, rcpt)

	status, _ := e.Track(t, tn.StatsHost(), trackingPath(t, pixelPath, msg.Body))
	assert.Equal(t, http.StatusOK, status)

	opened := tn.Await(t, rcpt, Opened)
	assert.Equal(t, "identified", opened.TrackingMode)
	assert.Empty(t, opened.Data.IP, "an Identified open retains no IP address")
	assert.Empty(t, opened.Data.UserAgent, "an Identified open retains no user agent")
}

func testClicked(t *testing.T, e *env) {
	tn := e.NewTenant(t)
	rcpt := tests.FakeEmail(t)

	tn.Send(t, tn.Batch("Clicked Test", trackedHTML, To(rcpt, nil)))
	msg := e.MX.AwaitEnvelope(t, rcpt)

	status, location := e.Track(t, tn.StatsHost(), trackingPath(t, linkPath, msg.Body))
	assert.Equal(t, http.StatusTemporaryRedirect, status)
	assert.Equal(t, landingURL, location, "the click redirects to the originally-authored URL")

	clicked := tn.Await(t, rcpt, Clicked)
	assert.Equal(t, landingURL, clicked.Data.URL)
	assert.Empty(t, clicked.Data.IP, "an Identified click retains no IP address")
	assert.Empty(t, clicked.Data.UserAgent, "an Identified click retains no user agent")
}

// testTrackingOff: a Domain Off on both axes sends mail with no tracking in it at all.
func testTrackingOff(t *testing.T, e *env) {
	tn := e.NewTenant(t)
	tn.SetTracking(t, restclient.Off, restclient.Off)
	rcpt := tests.FakeEmail(t)

	tn.Send(t, tn.Batch("Tracking Off Test", trackedHTML, To(rcpt, nil)))
	msg := e.MX.AwaitEnvelope(t, rcpt)

	assert.NotContains(t, msg.Body, "<img", "no tracking pixel is injected")
	assert.Contains(t, msg.Body, fmt.Sprintf("href=%q", landingURL), "the authored link is delivered unrewritten")
	assert.NotContains(t, msg.Body, tn.StatsHost(), "an untracked message carries no tracking hostname")
}

func testTrackingIdentified(t *testing.T, e *env) {
	tn := e.NewTenant(t)
	tn.SetTracking(t, restclient.Identified, restclient.Identified)

	opened, clicked := engage(t, e, tn, "Tracking Identified Test")

	assert.Empty(t, opened.Data.IP, "an Identified open retains no IP address")
	assert.Empty(t, opened.Data.UserAgent, "an Identified open retains no user agent")
	assert.Empty(t, clicked.Data.IP, "an Identified click retains no IP address")
	assert.Empty(t, clicked.Data.UserAgent, "an Identified click retains no user agent")
}

// testTrackingFull: the only Mode under which Kannon keeps anything about the request itself.
func testTrackingFull(t *testing.T, e *env) {
	tn := e.NewTenant(t)
	tn.SetTracking(t, restclient.Full, restclient.Full)

	opened, clicked := engage(t, e, tn, "Tracking Full Test")

	assert.Equal(t, "full", opened.TrackingMode)
	assert.Equal(t, engagementIP, opened.Data.IP)
	assert.Equal(t, engagementUserAgent, opened.Data.UserAgent)
	assert.Equal(t, engagementIP, clicked.Data.IP)
	assert.Equal(t, engagementUserAgent, clicked.Data.UserAgent)
}

// testTrackingAnonymous: a Domain on Anonymous keeps its rates while retaining nothing that isolates
// one Recipient. Two Recipients of one Batch receive the same tokens, since two minted ones would
// tell them apart.
func testTrackingAnonymous(t *testing.T, e *env) {
	tn := e.NewTenant(t)
	tn.SetTracking(t, restclient.Anonymous, restclient.Anonymous)
	first, second := tests.FakeEmail(t), tests.FakeEmail(t)

	tn.Send(t, tn.Batch("Tracking Anonymous Test", trackedHTML, To(first, nil), To(second, nil)))
	firstMsg := e.MX.AwaitEnvelope(t, first)
	secondMsg := e.MX.AwaitEnvelope(t, second)

	pixel := trackingPath(t, pixelPath, firstMsg.Body)
	link := trackingPath(t, linkPath, firstMsg.Body)
	assert.Equal(t, pixel, trackingPath(t, pixelPath, secondMsg.Body), "an anonymous pixel names nobody")
	assert.Equal(t, link, trackingPath(t, linkPath, secondMsg.Body), "an anonymous link names nobody")

	status, _ := e.Track(t, tn.StatsHost(), pixel)
	assert.Equal(t, http.StatusOK, status)
	status, location := e.Track(t, tn.StatsHost(), link)
	assert.Equal(t, http.StatusTemporaryRedirect, status)
	assert.Equal(t, landingURL, location, "the redirect is owed to the recipient under every Mode")

	eventually(t, "anonymous engagement counted in aggregate", func(c *probe) {
		counts := tn.Aggregated(c)
		assert.Equal(c, 1, counts[Opened])
		assert.Equal(c, 1, counts[Clicked])
	})
	tn.Holds(t, "no per-recipient engagement event", func(c *probe) {
		for _, ev := range tn.Events(c) {
			assert.NotEqual(c, Opened, ev.Type, "an anonymous open leaves no event")
			assert.NotEqual(c, Clicked, ev.Type, "an anonymous click leaves no event")
		}
	})
}

// testMixedRecipientTracking is the Recipient level of the cascade: one Batch, three Recipients
// stating three different things. Stating nothing is tracked at the Domain's level, Off narrows, and
// Full — above the ceiling — Rejects that one Recipient alone.
func testMixedRecipientTracking(t *testing.T, e *env) {
	tn := e.NewTenant(t)
	tn.SetTracking(t, restclient.Identified, restclient.Identified)
	tracked, untracked, greedy := tests.FakeEmail(t), tests.FakeEmail(t), tests.FakeEmail(t)

	off, full := "off", "full"
	untrackedIn := To(untracked, nil)
	untrackedIn.Tracking = &restclient.RecipientTracking{Opens: &off, Links: &off}
	greedyIn := To(greedy, nil)
	greedyIn.Tracking = &restclient.RecipientTracking{Opens: &full, Links: &full}

	b := tn.Send(t, tn.Batch("Mixed Recipient Tracking Test", trackedHTML, To(tracked, nil), untrackedIn, greedyIn))

	assert.Equal(t, 2, b.AcceptedCount, "the rest of the Batch proceeds")
	assert.Equal(t, 1, b.RejectedCount)
	require.Len(t, b.RejectedRecipients, 1)
	assert.Equal(t, 2, b.RejectedRecipients[0].Index)
	assert.Equal(t, "tracking_above_ceiling", b.RejectedRecipients[0].Code, "the caller is told why")

	trackedMsg := e.MX.AwaitEnvelope(t, tracked)
	assert.NotContains(t, trackedMsg.Body, fmt.Sprintf("href=%q", landingURL), "a tracked Recipient's links are rewritten")
	status, _ := e.Track(t, tn.StatsHost(), trackingPath(t, pixelPath, trackedMsg.Body))
	assert.Equal(t, http.StatusOK, status)
	tn.Await(t, tracked, Opened)

	untrackedMsg := e.MX.AwaitEnvelope(t, untracked)
	assert.NotContains(t, untrackedMsg.Body, "<img", "no pixel for a Recipient that refused")
	assert.Contains(t, untrackedMsg.Body, fmt.Sprintf("href=%q", landingURL), "the authored link is delivered unrewritten")
	assert.NotContains(t, untrackedMsg.Body, tn.StatsHost())

	tn.AwaitBatch(t, b, Completed)
	assert.Zero(t, e.MX.Attempts(greedy), "a Rejected Recipient has no Delivery and is never sent to")
}
