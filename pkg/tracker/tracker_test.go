package tracker

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/kannon-email/kannon/internal/tracking"
	"github.com/stretchr/testify/assert"
)

const (
	testMessageID  = "msg-1@test.com"
	testDomain     = "test.com"
	testRecipient  = "rcpt@example.com"
	testLandingURL = "https://example.com/landing"
	testUserAgent  = "kannon-test-agent/1.0"
	testIP         = "203.0.113.7"
	// testPseudonym has the shape ADR 0006 fixes for a Pseudonymous identity: 128
	// random bits as a lowercase hex local part, under the sending Domain's
	// reserved namespace. It is written out rather than drawn from
	// tracking.NewPseudonym because what is under test is what survives a token,
	// not how a pseudonym is generated.
	testPseudonym = "0f8b9d3c2a1e4f5b6c7d8e9f0a1b2c3d@track." + testDomain
)

// TestRetainedIsGatedOnMode states in one place the rule both handlers share, over
// the whole scale rather than only the Modes a token can currently be minted with.
//
// The line is Pseudonymous, not Identified: from that rung up the event keeps
// whatever identity its token claims, because an event that dropped its pseudonym
// would be linkable to nothing and the rung would record nothing at all. Below it
// the claim is dropped whatever it says, which is why the Modes that keep nothing
// are given a real address to drop: that is what a token minted by an older build
// carries, and the Tracker must not let it through even though the mint no longer
// produces one.
func TestRetainedIsGatedOnMode(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/o/tok", nil)
	req.Header.Set("User-Agent", testUserAgent)
	req.Header.Set("X-Real-Ip", testIP)

	cases := []struct {
		name    string
		mode    tracking.Mode
		claimed string
		want    engagement
	}{
		{name: "Off", mode: tracking.ModeOff, claimed: testRecipient, want: engagement{}},
		{name: "Anonymous", mode: tracking.ModeAnonymous, claimed: testRecipient, want: engagement{}},
		// The sentinel a current token carries under Anonymous is dropped on the
		// same terms: what it stands for is already said by the Mode.
		{
			name:    "AnonymousWithSentinelIdentity",
			mode:    tracking.ModeAnonymous,
			claimed: tracking.AnonymousIdentity(testDomain),
			want:    engagement{},
		},
		{
			name:    "Pseudonymous",
			mode:    tracking.ModePseudonymous,
			claimed: testPseudonym,
			want:    engagement{email: testPseudonym},
		},
		// Keeping the claim cannot invent one. A token minted before the identity
		// claim was always email-shaped left it empty under the Modes that name
		// nobody, and such tokens keep arriving for one token lifetime after this
		// change.
		{
			name:    "PseudonymousFromALegacyTokenWithNoIdentity",
			mode:    tracking.ModePseudonymous,
			claimed: "",
			want:    engagement{},
		},
		{
			name:    "Identified",
			mode:    tracking.ModeIdentified,
			claimed: testRecipient,
			want:    engagement{email: testRecipient},
		},
		{
			name:    "Full",
			mode:    tracking.ModeFull,
			claimed: testRecipient,
			want:    engagement{email: testRecipient, ip: testIP, userAgent: testUserAgent},
		},
		// An unstated Mode imposes no restriction of its own, so it keeps the
		// attribution its token was minted to carry — and still never reaches Full.
		{
			name:    "Unspecified",
			mode:    tracking.ModeUnspecified,
			claimed: testRecipient,
			want:    engagement{email: testRecipient},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, retained(req, tc.claimed, tc.mode))
		})
	}
}
