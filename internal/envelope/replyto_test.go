package envelope_test

import (
	"bytes"
	"net/mail"
	"testing"

	"github.com/kannon-email/kannon/internal/batch"
	"github.com/kannon-email/kannon/internal/envelope"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// buildWithReplyTo renders one Delivery of a Batch that states the given
// Reply-To and returns the message as a mail client would read it.
func buildWithReplyTo(t *testing.T, r batch.ReplyTo) *mail.Message {
	t.Helper()
	src := stubSource{data: envelope.SendingData{
		Subject:        "Welcome",
		HTML:           "<html><body>write back to us</body></html>",
		Domain:         "test.com",
		MessageID:      "msg-1",
		SenderEmail:    "noreply@test.com",
		SenderAlias:    "Test",
		DkimPrivateKey: newDKIMKeys(t),
		ReplyTo:        r,
	}}
	b := envelope.NewBuilderWith(src, stubTokens{link: "ltok", open: "otok"}, testReturnPath)

	env, err := b.Build(t.Context(), mustDelivery(t, "rcpt@example.com", nil))
	require.NoError(t, err)
	parsed, err := mail.ReadMessage(bytes.NewReader(env.Body()))
	require.NoError(t, err)
	return parsed
}

func TestBuilderWritesTheStatedReplyTo(t *testing.T) {
	parsed := buildWithReplyTo(t, batch.ReplyTo{Email: "maintainer@elsewhere.example", Alias: "The Maintainer"})

	assert.Equal(t, `"The Maintainer" <maintainer@elsewhere.example>`, parsed.Header.Get("Reply-To"))
	// Reply-To redirects replies; it does not change who the message is from.
	assert.Equal(t, `"Test" <noreply@test.com>`, parsed.Header.Get("From"))
}

func TestBuilderWritesAReplyToWithoutAlias(t *testing.T) {
	parsed := buildWithReplyTo(t, batch.ReplyTo{Email: "maintainer@elsewhere.example"})

	addr, err := mail.ParseAddress(parsed.Header.Get("Reply-To"))
	require.NoError(t, err)
	assert.Equal(t, "maintainer@elsewhere.example", addr.Address)
	assert.Empty(t, addr.Name)
}

func TestBuilderQuotesAReplyToAliasHoldingAComma(t *testing.T) {
	parsed := buildWithReplyTo(t, batch.ReplyTo{Email: "maintainer@elsewhere.example", Alias: "Rossi, Mario"})

	// Unquoted, the comma would make the header a list of two mailboxes.
	addrs, err := mail.ParseAddressList(parsed.Header.Get("Reply-To"))
	require.NoError(t, err)
	require.Len(t, addrs, 1)
	assert.Equal(t, "Rossi, Mario", addrs[0].Name)
	assert.Equal(t, "maintainer@elsewhere.example", addrs[0].Address)
}

func TestBuilderEncodesANonASCIIReplyToAlias(t *testing.T) {
	parsed := buildWithReplyTo(t, batch.ReplyTo{Email: "maintainer@elsewhere.example", Alias: "Ludovico Russò"})

	addr, err := mail.ParseAddress(parsed.Header.Get("Reply-To"))
	require.NoError(t, err)
	assert.Equal(t, "Ludovico Russò", addr.Name)
	assert.Equal(t, "maintainer@elsewhere.example", addr.Address)
}

func TestBuilderOmitsReplyToWhenNoneIsStated(t *testing.T) {
	parsed := buildWithReplyTo(t, batch.ReplyTo{})

	_, present := parsed.Header["Reply-To"]
	assert.False(t, present, "a Batch stating no Reply-To must not carry the header: replies go to From")
}
