package stats

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/kannon-email/kannon/internal/stats"
	"github.com/kannon-email/kannon/internal/tests"
	"github.com/kannon-email/kannon/internal/tracking"
	"github.com/kannon-email/kannon/internal/values"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/require"
)

func TestAnonymousRedeliveryAfterLostAckCountsOnce(t *testing.T) {
	domain := tests.FakeDomain(t)
	h := newTestHandler()
	msg := engagementEvent(t, domain, tracking.AnonymousIdentity(domain), tracking.ModeAnonymous)
	msg.ackErr = errors.New("ack connection lost")
	require.Error(t, h.handleAggregatedStatsMsg(t.Context(), msg))
	// Both consumers may see a retry; Anonymous still leaves no Recipient row.
	require.NoError(t, h.handleStatsMsg(t.Context(), engagementEvent(t, domain, tracking.AnonymousIdentity(domain), tracking.ModeAnonymous)))
	msg.ackErr = nil
	restarted := newTestHandler()
	require.NoError(t, restarted.handleAggregatedStatsMsg(t.Context(), msg))
	require.Equal(t, int64(1), aggregatedCount(t, domain, stats.TypeOpened))
	require.Zero(t, perRecipientRows(t, domain))
	// Another stored message with byte-identical payload is a genuine new event.
	msg.sequence = fixtureSequence.Add(1)
	require.NoError(t, restarted.handleAggregatedStatsMsg(t.Context(), msg))
	require.Equal(t, int64(2), aggregatedCount(t, domain, stats.TypeOpened))
	require.Zero(t, aggregatedCount(t, domain, stats.TypeClicked))
	require.Zero(t, perRecipientRows(t, domain))
}

func TestCountingNeedsStoredMessageMetadata(t *testing.T) {
	domain := tests.FakeDomain(t)
	h := newTestHandler()
	msg := engagementEvent(t, domain, "recipient@"+domain, tracking.ModeIdentified)
	msg.metadataErr = errors.New("metadata unavailable")
	require.NoError(t, h.handleAggregatedStatsMsg(t.Context(), msg))
	require.Equal(t, 1, msg.naked)
	require.Zero(t, msg.acked)
	require.Zero(t, aggregatedCount(t, domain, stats.TypeOpened))
	msg.metadataErr = nil
	require.NoError(t, h.handleAggregatedStatsMsg(t.Context(), msg))
	require.Equal(t, int64(1), aggregatedCount(t, domain, stats.TypeOpened))
}

func TestRecreatedStreamCountsReusedSequence(t *testing.T) {
	domain := tests.FakeDomain(t)
	h := newTestHandler()
	msg := engagementEvent(t, domain, "recipient@"+domain, tracking.ModeIdentified)
	require.NoError(t, h.handleAggregatedStatsMsg(t.Context(), msg))
	h.streamCreated = h.streamCreated.Add(time.Second)
	require.NoError(t, h.handleAggregatedStatsMsg(t.Context(), msg))
	require.Equal(t, int64(2), aggregatedCount(t, domain, stats.TypeOpened))
}

// Exercise the actual consumer setup: it must get the incarnation from the
// server, and a restarted worker must preserve it while a recreated stream must
// get a new one. The payload is byte-identical in all three deliveries.
func TestAggregatedConsumerBindsStreamIncarnation(t *testing.T) {
	js := tests.NatsJetStream(t)
	domain := tests.FakeDomain(t)
	event := engagementEvent(t, domain, tracking.AnonymousIdentity(domain), tracking.ModeAnonymous)
	repo := stats.NewInMemAggregatedStatsRepository()
	for i := range 3 {
		if i == 2 {
			require.NoError(t, js.DeleteStream(t.Context(), "kannon-stats"))
		}
		if i != 1 {
			_, err := js.CreateStream(t.Context(), jetstream.StreamConfig{Name: "kannon-stats", Subjects: []string{"kannon.stats.*"}})
			require.NoError(t, err)
			_, err = js.Publish(t.Context(), "kannon.stats.opened", event.data)
			require.NoError(t, err)
		}
		ctx, cancel := context.WithCancel(t.Context())
		t.Cleanup(cancel)
		h := statsHandler{js: js, counter: repo}
		done := make(chan error, 1)
		go func() { done <- h.handleAggregatedStats(ctx) }()
		// On a worker restart replay the retained message with a new consumer.
		// Its stored stream identity remains the same regardless of consumer state.
		expected := int64(1)
		if i == 2 {
			expected = 2
		}
		require.Eventually(t, func() bool {
			consumer, err := js.Consumer(t.Context(), "kannon-stats", "kannon-aggregated-stats")
			if err != nil {
				return false
			}
			info, err := consumer.Info(t.Context())
			if err != nil || info.AckFloor.Stream < 1 {
				return false
			}
			rows, err := repo.Query(t.Context(), values.MustParse(domain), stats.TimeRange{Start: time.Now().UTC().Add(-time.Hour), Stop: time.Now().UTC().Add(time.Hour)})
			return err == nil && len(rows) == 1 && rows[0].Count == expected
		}, 5*time.Second, 10*time.Millisecond)
		cancel()
		require.NoError(t, <-done)
		if i == 0 {
			require.NoError(t, js.DeleteConsumer(t.Context(), "kannon-stats", "kannon-aggregated-stats"))
		}
	}
}
