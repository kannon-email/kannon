package tracking_test

import (
	"strings"
	"testing"

	"github.com/kannon-email/kannon/internal/tracking"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestReservedNamespace pins the shape of the namespace an operator must keep
// free of real mailboxes: one label under the sending Domain, so the sentinel
// space and the deliverable space cannot collide (ADR 0006).
func TestReservedNamespace(t *testing.T) {
	assert.Equal(t, "track.kannon.dev", tracking.ReservedNamespace("kannon.dev"))
}

// TestAnonymousIdentity pins the one sentinel that is constant per Domain. It has
// to be: the whole Batch shares a single Anonymous token, so an identity that
// varied per Delivery would cost one RSA-4096 signature per Delivery to say
// nothing.
func TestAnonymousIdentity(t *testing.T) {
	first := tracking.AnonymousIdentity("kannon.dev")
	second := tracking.AnonymousIdentity("kannon.dev")

	assert.Equal(t, "anonymous@track.kannon.dev", first)
	assert.Equal(t, first, second)
	assert.True(t, tracking.InReservedNamespace(first, "kannon.dev"))
}

// TestNewPseudonym pins the two properties the Pseudonymous rung rests on: the
// pseudonym lives in the reserved namespace, and it is drawn fresh every time.
// Nothing derives it from the address, so no one — Kannon included — can link two
// of them or walk one back to a Recipient.
func TestNewPseudonym(t *testing.T) {
	first, err := tracking.NewPseudonym("kannon.dev")
	require.NoError(t, err)
	second, err := tracking.NewPseudonym("kannon.dev")
	require.NoError(t, err)

	assert.NotEqual(t, first, second)
	assert.True(t, tracking.InReservedNamespace(first, "kannon.dev"))

	local, host, ok := strings.Cut(first, "@")
	require.True(t, ok)
	assert.Equal(t, "track.kannon.dev", host)

	// 128 bits as lowercase hex: email pipelines case-fold, and a case-sensitive
	// alphabet would let a lowercasing in transit merge two pseudonyms into one.
	assert.Len(t, local, 32)
	assert.Equal(t, strings.ToLower(local), local)
	assert.Regexp(t, `^[0-9a-f]{32}$`, local)
}

// TestInReservedNamespace covers the check that keeps a real address from being
// minted under a Mode that must not name anybody.
func TestInReservedNamespace(t *testing.T) {
	cases := []struct {
		name     string
		identity string
		want     bool
	}{
		{name: "a pseudonym", identity: "0123456789abcdef0123456789abcdef@track.kannon.dev", want: true},
		{name: "the anonymous sentinel", identity: "anonymous@track.kannon.dev", want: true},
		// The case an operator would notice: a caller passing the recipient address
		// under a Mode that names nobody.
		{name: "a real address at the Domain", identity: "someone@kannon.dev", want: false},
		{name: "a real address elsewhere", identity: "someone@example.com", want: false},
		{name: "empty", identity: "", want: false},
		{name: "no local part", identity: "@track.kannon.dev", want: false},
		{name: "no at sign", identity: "track.kannon.dev", want: false},
		// A deeper subdomain is not the reserved namespace: only track.<fqdn> is
		// reserved, so anything below it may hold real mailboxes.
		{name: "a deeper subdomain", identity: "x@a.track.kannon.dev", want: false},
		// Another Domain's namespace is another Domain's, and a token minted for
		// this one may not carry it.
		{name: "another Domain's namespace", identity: "x@track.other.dev", want: false},
		// Email pipelines case-fold the domain part, so a sentinel that came back
		// uppercased is still the same sentinel.
		{name: "case-folded in transit", identity: "x@TRACK.KANNON.DEV", want: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, tracking.InReservedNamespace(tc.identity, "kannon.dev"))
		})
	}
}
