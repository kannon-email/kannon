package envelope

import (
	"testing"

	"github.com/kannon-email/kannon/internal/batch"
	"github.com/stretchr/testify/assert"
)

func TestBuildHeadersEmitsTheRFC8058Pair(t *testing.T) {
	sender := batch.Sender{Email: "noreply@test.com", Alias: "Test"}
	h := buildHeaders("subject", sender, "to@test.com", "132@test.com", "<msg-123@test.com>",
		headers{}, batch.Headers{}, "https://test.com/unsub?u=abc")

	assert.Equal(t, []string{"<https://test.com/unsub?u=abc>"}, h["List-Unsubscribe"],
		"RFC 2369 delimits the URI with angle brackets")
	assert.Equal(t, []string{"List-Unsubscribe=One-Click"}, h["List-Unsubscribe-Post"])
}

func TestBuildHeadersOmitsBothWhenNoEndpointIsStated(t *testing.T) {
	sender := batch.Sender{Email: "noreply@test.com", Alias: "Test"}
	h := buildHeaders("subject", sender, "to@test.com", "132@test.com", "<msg-123@test.com>",
		headers{}, batch.Headers{}, "")

	assert.NotContains(t, h, "List-Unsubscribe")
	assert.NotContains(t, h, "List-Unsubscribe-Post",
		"the Post header says nothing on its own and must never travel alone")
}

func TestResolveUnsubscribeURLPersonalisesPerDelivery(t *testing.T) {
	u := batch.OneClickUnsubscribe{URLTemplate: "https://test.com/unsub?email={{ email }}"}

	got := resolveUnsubscribeURL(u, map[string]string{"email": "mario+rossi@test.com"})

	assert.Equal(t, "https://test.com/unsub?email=mario%2Brossi%40test.com", got)
}

func TestResolveUnsubscribeURLOmitsRatherThanShipBroken(t *testing.T) {
	// The backstop of ADR 0005: intake should have refused this Recipient, so
	// reaching here means the check did not run. A message with no unsubscribe
	// header beats one advertising an authenticated endpoint that has braces in
	// it.
	u := batch.OneClickUnsubscribe{URLTemplate: "https://test.com/unsub?t={{ token }}"}

	got := resolveUnsubscribeURL(u, map[string]string{"email": "rcpt@test.com"})

	assert.Empty(t, got)
}

func TestResolveUnsubscribeURLIsEmptyWhenNoneIsStated(t *testing.T) {
	got := resolveUnsubscribeURL(batch.OneClickUnsubscribe{}, map[string]string{})

	assert.Empty(t, got)
}
