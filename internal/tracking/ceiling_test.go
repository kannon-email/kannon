package tracking_test

import (
	"testing"

	"github.com/kannon-email/kannon/internal/tracking"
	"github.com/stretchr/testify/assert"
)

func TestCeilingViolations(t *testing.T) {
	cases := []struct {
		name    string
		ceiling tracking.Policy
		stated  tracking.Policy
		want    []tracking.CeilingViolation
	}{
		{
			name:    "StatingNothingNeverViolates",
			ceiling: tracking.Policy{Opens: tracking.ModeOff, Links: tracking.ModeOff},
			stated:  tracking.Policy{},
			want:    nil,
		},
		{
			name:    "EqualIsAllowed",
			ceiling: tracking.Policy{Opens: tracking.ModeIdentified, Links: tracking.ModeIdentified},
			stated:  tracking.Policy{Opens: tracking.ModeIdentified, Links: tracking.ModeIdentified},
			want:    nil,
		},
		{
			name:    "BelowIsAllowed",
			ceiling: tracking.Policy{Opens: tracking.ModeFull, Links: tracking.ModeFull},
			stated:  tracking.Policy{Opens: tracking.ModeAnonymous, Links: tracking.ModeOff},
			want:    nil,
		},
		{
			name:    "AboveViolates",
			ceiling: tracking.Policy{Opens: tracking.ModeOff, Links: tracking.ModeOff},
			stated:  tracking.Policy{Opens: tracking.ModeFull, Links: tracking.ModeOff},
			want: []tracking.CeilingViolation{
				{Axis: tracking.AxisOpens, Ceiling: tracking.ModeOff, Stated: tracking.ModeFull},
			},
		},
		{
			name:    "AxesAreIndependentBothViolate",
			ceiling: tracking.Policy{Opens: tracking.ModeOff, Links: tracking.ModeAnonymous},
			stated:  tracking.Policy{Opens: tracking.ModeIdentified, Links: tracking.ModeFull},
			want: []tracking.CeilingViolation{
				{Axis: tracking.AxisOpens, Ceiling: tracking.ModeOff, Stated: tracking.ModeIdentified},
				{Axis: tracking.AxisLinks, Ceiling: tracking.ModeAnonymous, Stated: tracking.ModeFull},
			},
		},
		{
			name:    "AxesAreIndependentOnlyOneViolates",
			ceiling: tracking.Policy{Opens: tracking.ModeIdentified, Links: tracking.ModeOff},
			stated:  tracking.Policy{Opens: tracking.ModeAnonymous, Links: tracking.ModeFull},
			want: []tracking.CeilingViolation{
				{Axis: tracking.AxisLinks, Ceiling: tracking.ModeOff, Stated: tracking.ModeFull},
			},
		},
		{
			name:    "UnstatedCeilingNeverViolates",
			ceiling: tracking.Policy{},
			stated:  tracking.Policy{Opens: tracking.ModeFull, Links: tracking.ModeFull},
			want:    nil,
		},
		{
			name:    "UnknownStatedModeNeverViolates",
			ceiling: tracking.Policy{Opens: tracking.ModeOff, Links: tracking.ModeOff},
			stated:  tracking.Policy{Opens: tracking.Mode("shadow"), Links: tracking.ModeOff},
			want:    nil,
		},
		{
			// A ceiling this build cannot read must not become no ceiling at
			// all: the Domain's Policy is the only guarantee an operator has
			// (ADR 0003), so an unrecognised value is read as the floor of the
			// scale rather than as silence. Reachable when a newer build wrote
			// the row and this one is asked to enforce it.
			name:    "UnknownCeilingIsReadAsTheFloorNotAsSilence",
			ceiling: tracking.Policy{Opens: tracking.Mode("shadow"), Links: tracking.ModeFull},
			stated:  tracking.Policy{Opens: tracking.ModeAnonymous, Links: tracking.ModeFull},
			want: []tracking.CeilingViolation{
				{Axis: tracking.AxisOpens, Ceiling: tracking.Mode("shadow"), Stated: tracking.ModeAnonymous},
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := tracking.CeilingViolations(tc.ceiling, tc.stated)
			assert.Equal(t, tc.want, got)
		})
	}
}
