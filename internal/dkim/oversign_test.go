package dkim_test

import (
	"bytes"
	"strings"
	"testing"

	msgauth "github.com/emersion/go-msgauth/dkim"
	"github.com/kannon-email/kannon/internal/dkim"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// signedHeaders mirrors the set the Envelope Builder signs: fixed, naming
// headers whether or not the message carries them, and naming the two RFC 8058
// headers twice (ADR 0005).
var signedHeaders = []string{
	"From", "To", "Cc", "Subject", "Message-ID",
	"List-Unsubscribe", "List-Unsubscribe",
	"List-Unsubscribe-Post", "List-Unsubscribe-Post",
}

const plainMessage = "From: Test <noreply@test.com>\r\n" +
	"To: rcpt@example.com\r\n" +
	"Subject: Hello\r\n" +
	"Message-ID: <msg-1@test.com>\r\n" +
	"\r\n" +
	"hi\r\n"

func signAndVerify(t *testing.T, msg string, tamper func(signed string) string) []*msgauth.Verification {
	t.Helper()

	keys, err := dkim.GenerateDKIMKeysPair()
	require.NoError(t, err)

	signed, err := dkim.SignMessage(dkim.SignData{
		PrivateKey: keys.PrivateKey,
		Domain:     "test.com",
		Selector:   "kannon",
		Headers:    signedHeaders,
	}, bytes.NewReader([]byte(msg)))
	require.NoError(t, err)

	delivered := string(signed)
	if tamper != nil {
		delivered = tamper(delivered)
	}

	// The public key is served to the verifier directly, so the test needs no
	// DNS.
	verifications, err := msgauth.VerifyWithOptions(strings.NewReader(delivered), &msgauth.VerifyOptions{
		LookupTXT: func(string) ([]string, error) {
			return []string{"v=DKIM1; k=rsa; p=" + keys.PublicKey}, nil
		},
	})
	require.NoError(t, err)
	require.Len(t, verifications, 1)
	return verifications
}

// TestSignatureOverAbsentHeadersVerifies is the load-bearing check for the
// fixed h= list: a message carrying neither Cc nor the unsubscribe pair is
// signed naming all of them, and that signature must still verify. Were it not
// to, every message Kannon sends would arrive with dkim=fail.
func TestSignatureOverAbsentHeadersVerifies(t *testing.T) {
	v := signAndVerify(t, plainMessage, nil)

	assert.NoError(t, v[0].Err, "a signature naming absent headers must verify")
	assert.Equal(t, "test.com", v[0].Domain)
}

// TestAddingAnAbsentSignedHeaderBreaksTheSignature is the property the fixed
// list buys: an unsubscribe header injected in transit can no longer ride along
// unsigned, because the signer committed to its absence (RFC 6376 §5.4).
func TestAddingAnAbsentSignedHeaderBreaksTheSignature(t *testing.T) {
	v := signAndVerify(t, plainMessage, func(signed string) string {
		return "List-Unsubscribe: <https://attacker.example/unsub>\r\n" + signed
	})

	assert.Error(t, v[0].Err,
		"a List-Unsubscribe added after signing must invalidate the signature")
}

// TestAddingASecondCopyBreaksTheSignature is why the two List-* names appear
// twice. Instances are matched bottom-up, so with a single mention the original
// header would still hash correctly while an injected copy sat above it — and
// that copy is the one a client reads first.
func TestAddingASecondCopyBreaksTheSignature(t *testing.T) {
	withUnsubscribe := strings.Replace(plainMessage,
		"Subject: Hello\r\n",
		"Subject: Hello\r\nList-Unsubscribe: <https://sender.example/unsub>\r\n", 1)

	v := signAndVerify(t, withUnsubscribe, func(signed string) string {
		return "List-Unsubscribe: <https://attacker.example/unsub>\r\n" + signed
	})

	assert.Error(t, v[0].Err,
		"a second List-Unsubscribe prepended in transit must invalidate the signature")
}

// TestUnrelatedHeaderAdditionStillVerifies keeps the blast radius honest: only
// the named headers are committed to, so the X-* headers a relay routinely adds
// are still tolerated.
func TestUnrelatedHeaderAdditionStillVerifies(t *testing.T) {
	v := signAndVerify(t, plainMessage, func(signed string) string {
		return "X-Spam-Score: 0.1\r\n" + signed
	})

	assert.NoError(t, v[0].Err)
}
