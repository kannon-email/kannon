package trackingpb_test

import (
	"testing"

	"github.com/kannon-email/kannon/internal/tracking"
	"github.com/kannon-email/kannon/internal/trackingpb"
	pb "github.com/kannon-email/kannon/proto/kannon/tracking/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// wireFromANewerSchema is an enum value no build of Kannon defines. Proto3 enums
// are open, so a client built against a later schema can put one on the wire and
// this build has to decide what to do with it — which is the only strictness
// left in the translation once every Mode on the scale is honoured (#424).
const wireFromANewerSchema = pb.TrackingMode(9999)

// TestToPolicyReadsEveryModeOnTheScale pins that a Policy a client states is
// translated rather than filtered: every rung is a Mode an operator may ask for,
// including Pseudonymous, which was refused while it was reserved (ADR 0006).
func TestToPolicyReadsEveryModeOnTheScale(t *testing.T) {
	cases := []struct {
		name string
		wire pb.TrackingMode
		want tracking.Mode
	}{
		{"Unspecified", pb.TrackingMode_TRACKING_MODE_UNSPECIFIED, tracking.ModeUnspecified},
		{"Off", pb.TrackingMode_TRACKING_MODE_OFF, tracking.ModeOff},
		{"Anonymous", pb.TrackingMode_TRACKING_MODE_ANONYMOUS, tracking.ModeAnonymous},
		{"Pseudonymous", pb.TrackingMode_TRACKING_MODE_PSEUDONYMOUS, tracking.ModePseudonymous},
		{"Identified", pb.TrackingMode_TRACKING_MODE_IDENTIFIED, tracking.ModeIdentified},
		{"Full", pb.TrackingMode_TRACKING_MODE_FULL, tracking.ModeFull},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := trackingpb.ToPolicy(&pb.TrackingPolicy{Opens: tc.wire, Links: tc.wire})
			require.NoError(t, err)
			assert.Equal(t, tracking.Policy{Opens: tc.want, Links: tc.want}, got)

			// The two directions are one mapping stated once, so a Mode that
			// survives the round trip cannot drift between the APIs that read a
			// Policy and the ones that report it back.
			assert.Equal(t, tc.wire, trackingpb.FromMode(tc.want))
		})
	}
}

// TestToPolicyRefusesAModeFromANewerSchema covers the one value this build will
// not read: guessing what it meant would decide how much to retain about
// somebody without being asked, so the boundary refuses it and names the axis it
// arrived on.
func TestToPolicyRefusesAModeFromANewerSchema(t *testing.T) {
	t.Run("Opens", func(t *testing.T) {
		_, err := trackingpb.ToPolicy(&pb.TrackingPolicy{
			Opens: wireFromANewerSchema,
			Links: pb.TrackingMode_TRACKING_MODE_OFF,
		})
		require.ErrorIs(t, err, trackingpb.ErrUnknownMode)
		assert.Contains(t, err.Error(), "opens")
	})

	t.Run("Links", func(t *testing.T) {
		_, err := trackingpb.ToPolicy(&pb.TrackingPolicy{
			Opens: pb.TrackingMode_TRACKING_MODE_OFF,
			Links: wireFromANewerSchema,
		})
		require.ErrorIs(t, err, trackingpb.ErrUnknownMode)
		assert.Contains(t, err.Error(), "links")
	})
}

// TestToPolicyOfNilStatesNothing pins that an omitted Policy is silence, not a
// Policy of Off: the difference decides whether the level above is deferred to
// or overridden (ADR 0003).
func TestToPolicyOfNilStatesNothing(t *testing.T) {
	got, err := trackingpb.ToPolicy(nil)
	require.NoError(t, err)
	assert.Equal(t, tracking.Policy{}, got)
}

// TestToModeIsLenient pins the asymmetry with the stated side: a single Mode
// read here was written by Kannon itself, and a consumer that refused it would
// fail on data it cannot restate. An unreadable value reads as a Mode that
// states nothing, which can only ever restrict.
func TestToModeIsLenient(t *testing.T) {
	assert.Equal(t, tracking.ModeUnspecified, trackingpb.ToMode(wireFromANewerSchema))
	assert.Equal(t, tracking.ModePseudonymous, trackingpb.ToMode(pb.TrackingMode_TRACKING_MODE_PSEUDONYMOUS))
}
