package statssec

import (
	"testing"

	"github.com/golang-jwt/jwt/v5"
	"github.com/kannon-email/kannon/internal/tracking"
	"github.com/stretchr/testify/assert"
)

// TestAssertAudience pins the rule binding a token to the engagement channel it
// was minted for, including the transition rule for tokens minted before the
// Tracking Mode became a claim.
//
// It is tested here rather than through the service because a validly signed
// legacy token cannot be constructed from outside the package — rewriting a
// signed payload breaks the signature — and this is the whole of the rule.
func TestAssertAudience(t *testing.T) {
	cases := []struct {
		name     string
		audience jwt.ClaimStrings
		mode     tracking.Mode
		want     string
		accepted bool
	}{
		{
			name:     "OwnChannelIsAccepted",
			audience: jwt.ClaimStrings{audienceOpen},
			mode:     tracking.ModeIdentified,
			want:     audienceOpen,
			accepted: true,
		},
		{
			// The bypass this rule closes: a link token carries the Mode governing
			// links, so accepting it as an open would apply the more permissive of
			// a Domain's two axes to both endpoints.
			name:     "TheOtherChannelIsRefused",
			audience: jwt.ClaimStrings{audienceLink},
			mode:     tracking.ModeFull,
			want:     audienceOpen,
			accepted: false,
		},
		{
			// Minted before the Mode became a claim: still in inboxes for up to
			// the token lifetime after an upgrade, and refusing it would silently
			// drop every open and click from mail already in flight.
			name:     "LegacyAudienceStatingNoModeIsAccepted",
			audience: jwt.ClaimStrings{audienceLegacy},
			mode:     tracking.ModeUnspecified,
			want:     audienceOpen,
			accepted: true,
		},
		{
			// The transition rule's only loophole, closed: rewriting the audience
			// back to the legacy value to regain channel confusion must not also
			// keep a Mode. This build always signs one, so a Mode-bearing token
			// can never legitimately take the legacy path.
			name:     "LegacyAudienceStatingAModeIsRefused",
			audience: jwt.ClaimStrings{audienceLegacy},
			mode:     tracking.ModeFull,
			want:     audienceOpen,
			accepted: false,
		},
		{
			name:     "NoAudienceIsRefused",
			audience: nil,
			mode:     tracking.ModeIdentified,
			want:     audienceOpen,
			accepted: false,
		},
		{
			name:     "NoAudienceIsRefusedEvenStatingNoMode",
			audience: nil,
			mode:     tracking.ModeUnspecified,
			want:     audienceLink,
			accepted: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := assertAudience(tc.audience, tc.mode, tc.want)
			if tc.accepted {
				assert.NoError(t, err)
				return
			}
			assert.Error(t, err)
		})
	}
}
