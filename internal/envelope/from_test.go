package envelope_test

import (
	"bytes"
	"net/mail"
	"testing"

	"github.com/kannon-email/kannon/internal/envelope"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// buildWithSenderAlias renders one Delivery of a Batch sent under the given
// alias and returns the message as a mail client would read it.
func buildWithSenderAlias(t *testing.T, alias string) *mail.Message {
	t.Helper()
	src := stubSource{data: envelope.SendingData{
		Subject:        "Welcome",
		HTML:           "<html><body>hello</body></html>",
		Domain:         "test.com",
		MessageID:      "msg-1",
		SenderEmail:    "noreply@test.com",
		SenderAlias:    alias,
		DkimPrivateKey: newDKIMKeys(t),
	}}
	b := envelope.NewBuilderWith(src, stubTokens{link: "ltok", open: "otok"}, testReturnPath, 0)

	env, err := b.Build(t.Context(), mustDelivery(t, "rcpt@example.com", nil))
	require.NoError(t, err)
	parsed, err := mail.ReadMessage(bytes.NewReader(env.Body()))
	require.NoError(t, err)
	return parsed
}

func TestBuilderKeepsAnAliasHoldingAnAddressInsideTheDisplayName(t *testing.T) {
	alias := "Bank <security@bank.com>,"
	parsed := buildWithSenderAlias(t, alias)

	// Unquoted, the alias would put a second mailbox in From, and a client that
	// shows only the first would show the address the alias names.
	addrs, err := mail.ParseAddressList(parsed.Header.Get("From"))
	require.NoError(t, err)
	require.Len(t, addrs, 1)
	assert.Equal(t, "noreply@test.com", addrs[0].Address)
	assert.Equal(t, alias, addrs[0].Name)
}

func TestBuilderWritesAFromWithoutAlias(t *testing.T) {
	parsed := buildWithSenderAlias(t, "")

	assert.Equal(t, "<noreply@test.com>", parsed.Header.Get("From"))
}

func TestBuilderEncodesANonASCIISenderAlias(t *testing.T) {
	parsed := buildWithSenderAlias(t, "Ludovico Russò")

	addr, err := mail.ParseAddress(parsed.Header.Get("From"))
	require.NoError(t, err)
	assert.Equal(t, "Ludovico Russò", addr.Name)
	assert.Equal(t, "noreply@test.com", addr.Address)
}
