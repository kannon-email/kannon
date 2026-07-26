package tracking_test

import (
	"testing"

	"github.com/kannon-email/kannon/internal/tracking"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestModeRank(t *testing.T) {
	t.Run("OrderedScale", func(t *testing.T) {
		// The scale is ordered by increasing collection. It is asserted as a
		// sequence rather than as fixed numbers so a Mode may be inserted
		// mid-scale without rewriting the expectation.
		scale := []tracking.Mode{
			tracking.ModeOff,
			tracking.ModeAnonymous,
			tracking.ModePseudonymous,
			tracking.ModeIdentified,
			tracking.ModeFull,
		}

		for i := 1; i < len(scale); i++ {
			lower, lowerOK := scale[i-1].Rank()
			higher, higherOK := scale[i].Rank()
			require.True(t, lowerOK, "%q should be a stated Mode", scale[i-1])
			require.True(t, higherOK, "%q should be a stated Mode", scale[i])
			assert.Less(t, lower, higher, "%q should collect less than %q", scale[i-1], scale[i])
		}
	})

	t.Run("Unranked", func(t *testing.T) {
		cases := []struct {
			name string
			mode tracking.Mode
		}{
			{name: "Unspecified states nothing", mode: tracking.ModeUnspecified},
			{name: "Unknown value", mode: tracking.Mode("shadow")},
			{name: "Wrong case", mode: tracking.Mode("FULL")},
		}

		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				_, ok := tc.mode.Rank()
				assert.False(t, ok, "%q should have no position on the scale", tc.mode)
			})
		}
	})
}

func TestResolve(t *testing.T) {
	cases := []struct {
		name      string
		domain    tracking.Policy
		batch     tracking.Policy
		recipient tracking.Policy
		want      tracking.Policy
	}{
		{
			name: "AllUnstatedFallsBackToOff",
			want: tracking.Policy{Opens: tracking.ModeOff, Links: tracking.ModeOff},
		},
		{
			name:   "OnlyDomainStated",
			domain: tracking.Policy{Opens: tracking.ModeIdentified, Links: tracking.ModeIdentified},
			want:   tracking.Policy{Opens: tracking.ModeIdentified, Links: tracking.ModeIdentified},
		},
		{
			name:   "MostRestrictiveWins",
			domain: tracking.Policy{Opens: tracking.ModeFull, Links: tracking.ModeFull},
			batch:  tracking.Policy{Opens: tracking.ModeIdentified, Links: tracking.ModeIdentified},
			recipient: tracking.Policy{
				Opens: tracking.ModeAnonymous,
				Links: tracking.ModeAnonymous,
			},
			want: tracking.Policy{Opens: tracking.ModeAnonymous, Links: tracking.ModeAnonymous},
		},
		{
			name:      "MostRestrictiveIgnoresLevelOrder",
			domain:    tracking.Policy{Opens: tracking.ModeAnonymous, Links: tracking.ModeAnonymous},
			batch:     tracking.Policy{Opens: tracking.ModeFull, Links: tracking.ModeFull},
			recipient: tracking.Policy{Opens: tracking.ModeFull, Links: tracking.ModeFull},
			want:      tracking.Policy{Opens: tracking.ModeAnonymous, Links: tracking.ModeAnonymous},
		},
		{
			name:      "RecipientOffBeatsDomainFull",
			domain:    tracking.Policy{Opens: tracking.ModeFull, Links: tracking.ModeFull},
			recipient: tracking.Policy{Opens: tracking.ModeOff, Links: tracking.ModeOff},
			want:      tracking.Policy{Opens: tracking.ModeOff, Links: tracking.ModeOff},
		},
		{
			name:      "RecipientFullCannotBeatDomainOff",
			domain:    tracking.Policy{Opens: tracking.ModeOff, Links: tracking.ModeOff},
			recipient: tracking.Policy{Opens: tracking.ModeFull, Links: tracking.ModeFull},
			want:      tracking.Policy{Opens: tracking.ModeOff, Links: tracking.ModeOff},
		},
		{
			name:      "UnstatedBatchImposesNoRestriction",
			domain:    tracking.Policy{Opens: tracking.ModeFull, Links: tracking.ModeFull},
			recipient: tracking.Policy{Opens: tracking.ModeIdentified, Links: tracking.ModeIdentified},
			want:      tracking.Policy{Opens: tracking.ModeIdentified, Links: tracking.ModeIdentified},
		},
		{
			name:   "UnstatedRecipientImposesNoRestriction",
			domain: tracking.Policy{Opens: tracking.ModeFull, Links: tracking.ModeFull},
			batch:  tracking.Policy{Opens: tracking.ModeIdentified, Links: tracking.ModeIdentified},
			want:   tracking.Policy{Opens: tracking.ModeIdentified, Links: tracking.ModeIdentified},
		},
		{
			name:      "UnstatedDomainImposesNoRestriction",
			batch:     tracking.Policy{Opens: tracking.ModeFull, Links: tracking.ModeFull},
			recipient: tracking.Policy{Opens: tracking.ModeIdentified, Links: tracking.ModeIdentified},
			want:      tracking.Policy{Opens: tracking.ModeIdentified, Links: tracking.ModeIdentified},
		},
		{
			// A Mode this build does not recognise is read as off, never as
			// silence: silence defers to the level above, which would let an
			// unreadable value widen what is collected.
			name:   "UnknownModeIsReadAsOff",
			domain: tracking.Policy{Opens: tracking.ModeFull, Links: tracking.ModeFull},
			batch:  tracking.Policy{Opens: tracking.Mode("shadow"), Links: tracking.Mode("shadow")},
			want:   tracking.Policy{Opens: tracking.ModeOff, Links: tracking.ModeOff},
		},
		{
			// The case that matters in practice: the API boundary refuses
			// unknown wire values, so an unrecognised Mode reaches resolution
			// only from a Domain row written by a newer build. It must not
			// dissolve into silence and let the Batch decide.
			name:   "UnknownDomainModeDoesNotHandTheDecisionToTheBatch",
			domain: tracking.Policy{Opens: tracking.Mode("shadow"), Links: tracking.Mode("shadow")},
			batch:  tracking.Policy{Opens: tracking.ModeFull, Links: tracking.ModeFull},
			want:   tracking.Policy{Opens: tracking.ModeOff, Links: tracking.ModeOff},
		},
		{
			name:   "AxesResolveIndependently",
			domain: tracking.Policy{Opens: tracking.ModeFull, Links: tracking.ModeFull},
			batch:  tracking.Policy{Opens: tracking.ModeOff, Links: tracking.ModeIdentified},
			want:   tracking.Policy{Opens: tracking.ModeOff, Links: tracking.ModeIdentified},
		},
		{
			name:      "AxesDoNotBorrowFromEachOther",
			domain:    tracking.Policy{Opens: tracking.ModeAnonymous},
			recipient: tracking.Policy{Links: tracking.ModeFull},
			want:      tracking.Policy{Opens: tracking.ModeAnonymous, Links: tracking.ModeFull},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := tracking.Resolve(tc.domain, tc.batch, tc.recipient)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestPolicyNormalized(t *testing.T) {
	cases := []struct {
		name string
		in   tracking.Policy
		want tracking.Policy
	}{
		{
			name: "UnstatedBecomesOff",
			want: tracking.Policy{Opens: tracking.ModeOff, Links: tracking.ModeOff},
		},
		{
			name: "PerAxis",
			in:   tracking.Policy{Opens: tracking.ModeIdentified},
			want: tracking.Policy{Opens: tracking.ModeIdentified, Links: tracking.ModeOff},
		},
		{
			name: "StatedModesAreLeftAlone",
			in:   tracking.Policy{Opens: tracking.ModeAnonymous, Links: tracking.ModeFull},
			want: tracking.Policy{Opens: tracking.ModeAnonymous, Links: tracking.ModeFull},
		},
		{
			name: "UnknownModeBecomesOff",
			in:   tracking.Policy{Opens: tracking.Mode("shadow"), Links: tracking.ModeFull},
			want: tracking.Policy{Opens: tracking.ModeOff, Links: tracking.ModeFull},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, tc.in.Normalized())
		})
	}
}

// TestModeIdentifiesRecipient pins where on the scale attribution begins, which
// is the line the aggregate-statistics carve-out from the consent requirement
// follows: an event governed by a Mode below Identified may not name anybody.
func TestModeIdentifiesRecipient(t *testing.T) {
	cases := []struct {
		name string
		mode tracking.Mode
		want bool
	}{
		{name: "Off", mode: tracking.ModeOff, want: false},
		{name: "Anonymous", mode: tracking.ModeAnonymous, want: false},
		{name: "Pseudonymous", mode: tracking.ModePseudonymous, want: false},
		{name: "Identified", mode: tracking.ModeIdentified, want: true},
		{name: "Full", mode: tracking.ModeFull, want: true},
		// A Mode that states nothing imposes no restriction of its own, so it
		// leaves attribution exactly as it found it — the case is a token
		// minted before the Mode became a claim.
		{name: "Unspecified states nothing", mode: tracking.ModeUnspecified, want: true},
		// A Mode that states something unreadable is a different matter: it can
		// only come from a newer build, whose Mode may be more restrictive than
		// Identified, so it is read as the floor of the scale rather than as
		// permission to attribute.
		{name: "Unknown value", mode: tracking.Mode("shadow"), want: false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, tc.mode.IdentifiesRecipient())
		})
	}
}
