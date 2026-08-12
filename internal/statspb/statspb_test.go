package statspb_test

import (
	"testing"
	"time"

	"github.com/kannon-email/kannon/internal/stats"
	"github.com/kannon-email/kannon/internal/statspb"
	"github.com/kannon-email/kannon/internal/tracking"
	pbtypes "github.com/kannon-email/kannon/proto/kannon/stats/types"
	trackingtypes "github.com/kannon-email/kannon/proto/kannon/tracking/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// everyOutcome is the closed set CONTEXT.md lists under "Outcomes (per
// Delivery)", plus Errored, which is a retry signal rather than an outcome but
// travels the same topics. Every case carries non-zero values in every field
// the variant has, so a mapping that crossed two fields over could not pass.
var everyOutcome = []struct {
	name string
	out  stats.Outcome
	want *pbtypes.StatsData
}{
	{
		"Accepted",
		stats.Accepted(),
		&pbtypes.StatsData{Data: &pbtypes.StatsData_Accepted{Accepted: &pbtypes.StatsDataAccepted{}}},
	},
	{
		"Delivered",
		stats.Delivered(),
		&pbtypes.StatsData{Data: &pbtypes.StatsData_Delivered{Delivered: &pbtypes.StatsDataDelivered{}}},
	},
	{
		"Rejected",
		stats.Rejected("not a valid email"),
		&pbtypes.StatsData{Data: &pbtypes.StatsData_Rejected{
			Rejected: &pbtypes.StatsDataRejected{Reason: "not a valid email"},
		}},
	},
	{
		"Failed",
		stats.Failed("retry budget exhausted while dispatching"),
		&pbtypes.StatsData{Data: &pbtypes.StatsData_Failed{
			Failed: &pbtypes.StatsDataFailed{Reason: "retry budget exhausted while dispatching"},
		}},
	},
	{
		"Bounced",
		stats.Bounced(true, 550, "550 no such user"),
		&pbtypes.StatsData{Data: &pbtypes.StatsData_Bounced{
			Bounced: &pbtypes.StatsDataBounced{Permanent: true, Code: 550, Msg: "550 no such user"},
		}},
	},
	{
		// A 4xx bounce is terminal without being permanent, which is the
		// distinction #433 re-specified: the flag follows the reply class, never
		// the retry decision. The translation must carry it verbatim.
		"BouncedTransient",
		stats.Bounced(false, 450, "450 mailbox temporarily unavailable"),
		&pbtypes.StatsData{Data: &pbtypes.StatsData_Bounced{
			Bounced: &pbtypes.StatsDataBounced{Permanent: false, Code: 450, Msg: "450 mailbox temporarily unavailable"},
		}},
	},
	{
		"Errored",
		stats.Errored(421, "451 try again later"),
		&pbtypes.StatsData{Data: &pbtypes.StatsData_Error{
			Error: &pbtypes.StatsDataError{Code: 421, Msg: "451 try again later"},
		}},
	},
	{
		"Opened",
		stats.Opened("Mozilla/5.0", "203.0.113.7"),
		&pbtypes.StatsData{Data: &pbtypes.StatsData_Opened{
			Opened: &pbtypes.StatsDataOpened{UserAgent: "Mozilla/5.0", Ip: "203.0.113.7"},
		}},
	},
	{
		"Clicked",
		stats.Clicked("Mozilla/5.0", "203.0.113.7", "https://example.com/offer"),
		&pbtypes.StatsData{Data: &pbtypes.StatsData_Clicked{
			Clicked: &pbtypes.StatsDataClicked{
				UserAgent: "Mozilla/5.0",
				Ip:        "203.0.113.7",
				Url:       "https://example.com/offer",
			},
		}},
	},
}

// TestOutcomeRoundTrip pins the field mapping in both directions for every
// outcome. The wire form is asserted literally rather than only round-tripped,
// because a pair of translations that agreed with each other and with nothing
// else would still break every consumer already deployed.
func TestOutcomeRoundTrip(t *testing.T) {
	for _, tc := range everyOutcome {
		t.Run(tc.name, func(t *testing.T) {
			got := statspb.FromOutcome(tc.out)
			assert.True(t, proto.Equal(tc.want, got), "want %v, got %v", tc.want, got)
			assert.Equal(t, tc.out, statspb.ToOutcome(got))
		})
	}
}

// TestOutcomeTypesAreDistinct guards the switch in FromOutcome against two
// outcomes collapsing onto one wire variant, which a per-case round trip would
// not notice if the pair happened to be symmetric. The table holds nine cases
// for eight types: the two Bounced ones differ only in reply class.
func TestOutcomeTypesAreDistinct(t *testing.T) {
	seen := make(map[stats.Type]bool, len(everyOutcome))
	for _, tc := range everyOutcome {
		seen[statspb.ToOutcome(statspb.FromOutcome(tc.out)).Type()] = true
	}
	assert.Len(t, seen, 8, "every outcome should translate to a type of its own")
	assert.NotContains(t, seen, stats.TypeUnknown, "no outcome should translate to Unknown")
}

// TestUnreadablePayloadsAreUnknown covers the three ways a payload can fail to
// name an outcome. All three read as the zero Outcome, and the zero Outcome
// renders as no payload at all — which is what a consumer that then Terms the
// message needs, and what the protobuf-inspecting predicate this replaced
// reported for a nil payload.
func TestUnreadablePayloadsAreUnknown(t *testing.T) {
	t.Run("nil", func(t *testing.T) {
		assert.Equal(t, stats.Outcome{}, statspb.ToOutcome(nil))
		assert.Equal(t, stats.TypeUnknown, statspb.ToOutcome(nil).Type())
	})

	t.Run("unset oneof", func(t *testing.T) {
		assert.Equal(t, stats.Outcome{}, statspb.ToOutcome(&pbtypes.StatsData{}))
	})

	t.Run("zero Outcome renders as no payload", func(t *testing.T) {
		assert.Nil(t, statspb.FromOutcome(stats.Outcome{}))
	})
}

// TestEventRoundTrip pins the envelope around the payload, including the
// Tracking Mode, which is translated through internal/trackingpb rather than a
// second copy of that mapping.
func TestEventRoundTrip(t *testing.T) {
	ts := time.Date(2026, 1, 15, 10, 30, 0, 0, time.UTC)

	event := stats.Event{
		MessageID:    "batch@example.com",
		Domain:       "example.com",
		Email:        "user@example.com",
		Timestamp:    ts,
		Outcome:      stats.Opened("Mozilla/5.0", "203.0.113.7"),
		TrackingMode: tracking.ModeFull,
	}

	msg := statspb.FromEvent(event)
	assert.Equal(t, "batch@example.com", msg.MessageId)
	assert.Equal(t, "example.com", msg.Domain)
	assert.Equal(t, "user@example.com", msg.Email)
	assert.Equal(t, timestamppb.New(ts), msg.Timestamp)
	assert.Equal(t, trackingtypes.TrackingMode_TRACKING_MODE_FULL, msg.TrackingMode)
	require.NotNil(t, msg.Data.GetOpened())

	assert.Equal(t, event, statspb.ToEvent(msg))
}

// TestEventOfAnUnstatedModeSaysNothing pins that a non-engagement outcome leaves
// the Mode unspecified: only opens and clicks come from an observed channel, and
// an unspecified Mode is silence rather than a Mode of Off.
func TestEventOfAnUnstatedModeSaysNothing(t *testing.T) {
	msg := statspb.FromEvent(stats.Event{
		MessageID: "batch@example.com",
		Domain:    "example.com",
		Email:     "user@example.com",
		Timestamp: time.Now(),
		Outcome:   stats.Delivered(),
	})

	assert.Equal(t, trackingtypes.TrackingMode_TRACKING_MODE_UNSPECIFIED, msg.TrackingMode)
	assert.Equal(t, tracking.ModeUnspecified, statspb.ToEvent(msg).TrackingMode)
}

// TestLegacyTypeFieldIsPreserved pins the one field this package fills that
// nothing reads. The Tracker has stamped Stats.type on engagement events since
// #420 and no other producer ever has; the asymmetry is reproduced verbatim so
// that moving the translation here changes no byte on the wire. When the field
// is finally dropped, this test is the one that should be deleted with it.
func TestLegacyTypeFieldIsPreserved(t *testing.T) {
	event := func(o stats.Outcome) *pbtypes.Stats {
		return statspb.FromEvent(stats.Event{
			MessageID: "batch@example.com",
			Domain:    "example.com",
			Email:     "user@example.com",
			Timestamp: time.Now(),
			Outcome:   o,
		})
	}

	assert.Equal(t, "opened", event(stats.Opened("", "")).Type)
	assert.Equal(t, "clicked", event(stats.Clicked("", "", "https://example.com")).Type)

	for _, o := range []stats.Outcome{
		stats.Accepted(), stats.Delivered(), stats.Rejected("r"),
		stats.Failed("r"), stats.Bounced(true, 550, "m"), stats.Errored(421, "m"),
	} {
		assert.Empty(t, event(o).Type,
			"%s has never carried the redundant type field", o.Type())
	}
}
