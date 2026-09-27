package returnpath_test

import (
	"encoding/base64"
	"strings"
	"testing"

	"github.com/kannon-email/kannon/internal/returnpath"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	secret    = "0123456789abcdef0123456789abcdef"
	messageID = "msg_cl6g7ndft0001018ut5octeun@k.test.com"
)

var signer = returnpath.MustParse(secret)

// Build and Parse are pinned together across the URL-safe alphabet: the two used to disagree on
// the symbols where it differs from the standard one ('-'/'_' versus '+'/'/'), and every address
// whose encoding hit one of them produced a return path whose bounce was silently dropped (#432).
func TestRoundTrip(t *testing.T) {
	emails := []string{
		"test@test.com", // plain: both alphabets agree
		"ab~@test.com",  // encodes to '-' under URL-safe, '+' under standard
		"aÿ@test.com",   // non-ASCII (SMTPUTF8): '_' under URL-safe, '/' under standard
	}

	for _, want := range emails {
		t.Run(want, func(t *testing.T) {
			rp := signer.Build(want, messageID)

			email, gotMessageID, domain, found, err := signer.Parse(rp)

			require.NoError(t, err)
			assert.True(t, found)
			assert.Equal(t, want, email)
			assert.Equal(t, messageID, gotMessageID)
			assert.Equal(t, "k.test.com", domain)
		})
	}
}

// The fixtures above only exercise the alphabet if their encodings actually land on the divergent
// symbols; check that rather than assume it.
func TestRoundTripFixturesHitTheDivergentSymbols(t *testing.T) {
	assert.Contains(t, base64.URLEncoding.EncodeToString([]byte("ab~@test.com")), "-")
	assert.Contains(t, base64.URLEncoding.EncodeToString([]byte("aÿ@test.com")), "_")
}

func TestParseIgnoresAnAddressThatIsNotABouncePath(t *testing.T) {
	//nolint:dogsled
	_, _, _, found, err := signer.Parse("someone@test.com")
	assert.NoError(t, err)
	assert.False(t, found)
}

// What an attacker can put together from a message they received — the Batch ID from its headers
// and any address they like, base64-encoded — is exactly the unsigned return path Kannon used to
// accept.
func TestParseRefusesAnUnsignedReturnPath(t *testing.T) {
	forged := "bump_" + base64.URLEncoding.EncodeToString([]byte("victim@test.com")) + "+" + messageID

	//nolint:dogsled
	_, _, _, found, err := signer.Parse(forged)
	assert.True(t, found, "it has the shape of a bounce path, so it is found")
	assert.ErrorIs(t, err, returnpath.ErrInvalidSignature)
}

// A signature over one Recipient vouches for no other: moving it onto another address, or onto
// another Batch, is refused.
func TestParseRefusesASignatureMovedToAnotherDelivery(t *testing.T) {
	rp := signer.Build("attacker@test.com", messageID)
	_, macAndRest, _ := strings.Cut(rp, ".")
	mac, _, _ := strings.Cut(macAndRest, "+")

	otherRecipient := "bump_" + base64.URLEncoding.EncodeToString([]byte("victim@test.com")) + "." + mac + "+" + messageID
	otherBatch := strings.Replace(rp, messageID, "msg_other@k.test.com", 1)

	for name, forged := range map[string]string{"recipient": otherRecipient, "batch": otherBatch} {
		t.Run(name, func(t *testing.T) {
			_, _, _, _, err := signer.Parse(forged)
			assert.ErrorIs(t, err, returnpath.ErrInvalidSignature)
		})
	}
}

func TestParseRefusesAReturnPathSignedWithAnotherSecret(t *testing.T) {
	other := returnpath.MustParse("ffffffffffffffffffffffffffffffff")

	//nolint:dogsled
	_, _, _, _, err := signer.Parse(other.Build("test@test.com", messageID))
	assert.ErrorIs(t, err, returnpath.ErrInvalidSignature)
}

func TestTheZeroSignerVerifiesNothing(t *testing.T) {
	var zero returnpath.Signer

	//nolint:dogsled
	_, _, _, _, err := zero.Parse(signer.Build("test@test.com", messageID))
	assert.ErrorIs(t, err, returnpath.ErrInvalidSignature)
	assert.Panics(t, func() { zero.Build("test@test.com", messageID) })
}

func TestParseSecret(t *testing.T) {
	_, err := returnpath.Parse("too-short")
	assert.Error(t, err)

	_, err = returnpath.Parse("   " + strings.Repeat(" ", returnpath.MinSecretLength) + "\n")
	assert.Error(t, err, "whitespace is trimmed before the length is checked")

	a := returnpath.MustParse(secret + "\n")
	_, _, _, _, err = signer.Parse(a.Build("test@test.com", messageID)) //nolint:dogsled
	assert.NoError(t, err, "a trailing newline from a mounted Secret is not part of the key")
}
