package stats

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"
	"time"

	sq "github.com/kannon-email/kannon/internal/db"
	"github.com/kannon-email/kannon/internal/stats"
	"github.com/kannon-email/kannon/internal/tests"
	"github.com/kannon-email/kannon/internal/tracking"
	"github.com/kannon-email/kannon/internal/trackingpb"
	"github.com/kannon-email/kannon/proto/kannon/stats/types"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// fakeMsg stands in for a JetStream message so a test can assert how the handler
// settles it. Which of Ack, Term and Nak is called is the difference between a
// message that is finished with, one that is abandoned on purpose, and one that
// comes straight back — and a permanent fault that comes back is the redelivery
// hot loop #396 fixed, so the distinction is worth pinning.
//
// jetstream.Msg is embedded to pick up the parts of the interface the handler does
// not touch; calling one of those would panic loudly rather than pass silently.
type fakeMsg struct {
	jetstream.Msg
	data   []byte
	acked  int
	termed int
	naked  int
}

func (m *fakeMsg) Data() []byte { return m.data }
func (m *fakeMsg) Ack() error   { m.acked++; return nil }
func (m *fakeMsg) Term() error  { m.termed++; return nil }
func (m *fakeMsg) Nak() error   { m.naked++; return nil }

func (m *fakeMsg) NakWithDelay(time.Duration) error { m.naked++; return nil }

// engagementEvent is one Opened event as the Tracker publishes it, under the given
// Mode and carrying the given identity claim: the Recipient's address, a pseudonym,
// the Anonymous sentinel of the Domain — or nothing at all, when the caller passes
// "", which is what a token minted before the claim was always email-shaped carries.
func engagementEvent(t *testing.T, domain, email string, mode tracking.Mode) *fakeMsg {
	t.Helper()

	data, err := proto.Marshal(&types.Stats{
		MessageId:    "batch@" + domain,
		Email:        email,
		Domain:       domain,
		Type:         "opened",
		TrackingMode: trackingpb.FromMode(mode),
		Timestamp:    timestamppb.Now(),
		Data: &types.StatsData{
			Data: &types.StatsData_Opened{Opened: &types.StatsDataOpened{}},
		},
	})
	require.NoError(t, err)

	return &fakeMsg{data: data}
}

// perRecipientRows counts the rows the per-recipient consumer left behind for a
// Domain — what an operator reading the stats table would find.
func perRecipientRows(t *testing.T, domain string) int64 {
	t.Helper()

	// An event timestamps itself in UTC, and the column is timezone-naive, so the
	// window has to be stated in UTC too or it misses everything by the offset.
	repo := sq.NewStatsRepository(db)
	total, err := repo.Count(t.Context(), domain, stats.TimeRange{
		Start: time.Now().UTC().Add(-time.Hour),
		Stop:  time.Now().UTC().Add(time.Hour),
	})
	require.NoError(t, err)
	return total
}

// perRecipientIdentities returns the identity every row the per-recipient consumer
// left behind for a Domain was written under — the addresses an operator reading
// the stats table would find in it.
func perRecipientIdentities(t *testing.T, domain string) []string {
	t.Helper()

	repo := sq.NewStatsRepository(db)
	rows, err := repo.Query(t.Context(), domain, stats.TimeRange{
		Start: time.Now().UTC().Add(-time.Hour),
		Stop:  time.Now().UTC().Add(time.Hour),
	}, stats.Pagination{Limit: 100, Offset: 0})
	require.NoError(t, err)

	identities := make([]string, 0, len(rows))
	for _, row := range rows {
		identities = append(identities, row.Email)
	}
	return identities
}

// aggregatedCount reports a Domain's aggregated counter for one stat type,
// summed over the hourly buckets around now.
func aggregatedCount(t *testing.T, domain string, statType stats.Type) int64 {
	t.Helper()

	repo := sq.NewAggregatedStatsRepository(db)
	rows, err := repo.Query(t.Context(), domain, stats.TimeRange{
		Start: time.Now().UTC().Add(-48 * time.Hour),
		Stop:  time.Now().UTC().Add(48 * time.Hour),
	})
	require.NoError(t, err)

	var total int64
	for _, row := range rows {
		if row.Type == statType {
			total += row.Count
		}
	}
	return total
}

// TestAnonymousEventIsCountedButNotRecorded is the aggregate-statistics carve-out
// made observable: a Domain on Anonymous keeps its open rate and retains no row
// that could isolate one Recipient from another. Both consumers see the same
// message, as they do in production, so the two halves are asserted together.
//
// It is run over both identity claims an Anonymous event can arrive with — the
// Domain's sentinel, which is what a token minted by this build carries (ADR 0006),
// and nothing at all, which is what a token minted before the claim was always
// email-shaped carries and will keep carrying for one token lifetime. Neither may
// become a row.
func TestAnonymousEventIsCountedButNotRecorded(t *testing.T) {
	cases := []struct {
		name     string
		identity func(domain string) string
	}{
		{name: "SentinelIdentity", identity: tracking.AnonymousIdentity},
		{name: "LegacyTokenWithNoIdentity", identity: func(string) string { return "" }},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			domain := tests.FakeDomain(t)
			h := newTestHandler()

			msg := engagementEvent(t, domain, tc.identity(domain), tracking.ModeAnonymous)
			require.NoError(t, h.handleStatsMsg(t.Context(), msg))
			assert.Equal(t, 1, msg.acked, "an anonymous event is finished with, not left to come back")

			assert.Zero(t, perRecipientRows(t, domain),
				"an anonymous event must leave no per-recipient row")

			counted := engagementEvent(t, domain, tc.identity(domain), tracking.ModeAnonymous)
			require.NoError(t, h.handleAggregatedStatsMsg(t.Context(), counted))
			assert.Equal(t, int64(1), aggregatedCount(t, domain, stats.TypeOpened),
				"an anonymous event must still be counted in aggregate")
		})
	}
}

// TestPseudonymousEventIsRecordedUnderItsPseudonym is what separates the rung from
// Anonymous at the write path. A Pseudonymous event names no Recipient, but it must
// leave a per-Delivery row all the same: being linkable to the Batch's other events
// is the whole content of the rung, and the pseudonym is the only thing that makes
// them so.
//
// It takes the existing per-recipient path with no special case, which is what the
// identity claim being email-shaped whichever Mode produced it buys (ADR 0006): the
// row, the schema and counting distinct addresses are untouched by the Mode.
func TestPseudonymousEventIsRecordedUnderItsPseudonym(t *testing.T) {
	domain := tests.FakeDomain(t)
	h := newTestHandler()

	pseudonym, err := tracking.NewPseudonym(domain)
	require.NoError(t, err)

	msg := engagementEvent(t, domain, pseudonym, tracking.ModePseudonymous)
	require.NoError(t, h.handleStatsMsg(t.Context(), msg))
	assert.Equal(t, 1, msg.acked)
	assert.Zero(t, msg.termed, "a pseudonym is an identity, not a missing one")

	assert.Equal(t, []string{pseudonym}, perRecipientIdentities(t, domain),
		"a pseudonymous event must be recorded, and under the pseudonym it arrived with")

	// The aggregate path reads only the Domain, the timestamp and the type, so it
	// counts every Mode alike. Pinning it here is what makes "the Domain's
	// counters work under pseudonymous as they do under every Mode" a fact rather
	// than an inference from the code.
	counted := engagementEvent(t, domain, pseudonym, tracking.ModePseudonymous)
	require.NoError(t, h.handleAggregatedStatsMsg(t.Context(), counted))
	assert.Equal(t, int64(1), aggregatedCount(t, domain, stats.TypeOpened),
		"a pseudonymous event must move the Domain's aggregate counters")
}

// TestTwoPseudonymousEventsOfABatchStayApart is the other half of the rung, from
// the operator's side: two Deliveries of one Batch carry two pseudonyms, so their
// events are as distinguishable as identified ones would be — while naming nobody.
func TestTwoPseudonymousEventsOfABatchStayApart(t *testing.T) {
	domain := tests.FakeDomain(t)
	h := newTestHandler()

	first, err := tracking.NewPseudonym(domain)
	require.NoError(t, err)
	second, err := tracking.NewPseudonym(domain)
	require.NoError(t, err)

	for _, pseudonym := range []string{first, second} {
		msg := engagementEvent(t, domain, pseudonym, tracking.ModePseudonymous)
		require.NoError(t, h.handleStatsMsg(t.Context(), msg))
	}

	assert.ElementsMatch(t, []string{first, second}, perRecipientIdentities(t, domain),
		"the two Deliveries must be told apart by their pseudonyms and nothing else")
}

// TestIdentifiedEventIsRecorded is the control: the skip is about Anonymous and
// not about engagement events in general.
func TestIdentifiedEventIsRecorded(t *testing.T) {
	domain := tests.FakeDomain(t)
	h := newTestHandler()

	msg := engagementEvent(t, domain, "rcpt@"+domain, tracking.ModeIdentified)
	require.NoError(t, h.handleStatsMsg(t.Context(), msg))
	assert.Equal(t, 1, msg.acked)

	assert.Equal(t, int64(1), perRecipientRows(t, domain),
		"an identified event must be attributed to its Recipient")
}

// TestNonAnonymousEventWithoutIdentityIsLoggedAsAnError is the invariant asserted
// rather than assumed. An event that is not Anonymous is supposed to name its
// Recipient; one that does not is a bug upstream, and in a compliance path losing
// a row in silence is worse than saying so.
//
// It must not come back, either: the fault is in the message, so a Nak would only
// buy a redelivery hot loop (#396).
func TestNonAnonymousEventWithoutIdentityIsLoggedAsAnError(t *testing.T) {
	domain := tests.FakeDomain(t)
	h := newTestHandler()

	logged := captureLogs(t)

	msg := engagementEvent(t, domain, "", tracking.ModeIdentified)
	require.NoError(t, h.handleStatsMsg(t.Context(), msg))

	assert.Zero(t, msg.naked, "a permanent fault must not be sent round again")
	assert.Equal(t, 1, msg.termed, "the message must be settled, not left in flight")
	assert.Zero(t, perRecipientRows(t, domain), "there is no identity to attribute a row to")

	out := logged()
	assert.Contains(t, out, `"level":"ERROR"`, "the violation must be logged as an error, got %q", out)
	assert.Contains(t, out, domain, "the log must say which Domain, got %q", out)
}

// TestNonAnonymousEventNamingTheAnonymousSentinelIsLoggedAsAnError is the same
// invariant against the shape the identity claim now has. Since the Anonymous
// sentinel is an ordinary address to the `stats` schema, an event carrying it under
// a Mode that is not Anonymous would otherwise be written down as though somebody
// were called `anonymous@track.<domain>` — a Recipient that does not exist, in a
// table whose distinct addresses are supposed to count Recipients. Naming nobody is
// therefore a question about the address, not merely about emptiness.
func TestNonAnonymousEventNamingTheAnonymousSentinelIsLoggedAsAnError(t *testing.T) {
	domain := tests.FakeDomain(t)
	h := newTestHandler()

	logged := captureLogs(t)

	msg := engagementEvent(t, domain, tracking.AnonymousIdentity(domain), tracking.ModeIdentified)
	require.NoError(t, h.handleStatsMsg(t.Context(), msg))

	assert.Zero(t, msg.naked, "a permanent fault must not be sent round again")
	assert.Equal(t, 1, msg.termed, "the message must be settled, not left in flight")
	assert.Zero(t, perRecipientRows(t, domain), "the sentinel must never be recorded as a Recipient")

	out := logged()
	assert.Contains(t, out, `"level":"ERROR"`, "the violation must be logged as an error, got %q", out)
}

// captureLogs redirects the default logger for the duration of one test and
// returns everything written to it, so a test can assert that an operator is in
// fact told about the invariant violation.
func captureLogs(t *testing.T) func() string {
	t.Helper()

	buf := &bytes.Buffer{}
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(previous) })

	return func() string {
		return strings.TrimSpace(buf.String())
	}
}
