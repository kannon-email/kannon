package mailapi_test

import (
	"testing"

	"connectrpc.com/connect"
	"github.com/kannon-email/kannon/internal/tests"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/timestamppb"

	mailerv1 "github.com/kannon-email/kannon/proto/kannon/mailer/apiv1"
	types "github.com/kannon-email/kannon/proto/kannon/mailer/types"
)

// sendWithUnsubscribe performs one send stating an unsubscribe endpoint, and
// returns whatever the API returned — success or failure — so a test can assert
// on either.
func sendWithUnsubscribe(t *testing.T, d *tests.DomainWithKey, urlTemplate string, recipients ...*types.Recipient) (*mailerv1.SendRes, error) {
	t.Helper()
	req := connect.NewRequest(&mailerv1.SendHTMLReq{
		Sender: &types.Sender{
			Email: "test@" + d.Domain.Domain,
			Alias: "Test",
		},
		Recipients:          recipients,
		Subject:             "Test",
		Html:                `<p>Hello</p>`,
		ScheduledTime:       timestamppb.Now(),
		OneClickUnsubscribe: &types.OneClickUnsubscribe{UrlTemplate: urlTemplate},
	})
	authRequest(req, d)

	res, err := ts.SendHTML(t.Context(), req)
	if err != nil {
		return nil, err
	}
	return res.Msg, nil
}

// TestSendAcceptsAnUnsubscribeResolvableFromTheInjectedAddress covers the case
// that needs no per-Recipient data at all: `email` is supplied by Kannon.
func TestSendAcceptsAnUnsubscribeResolvableFromTheInjectedAddress(t *testing.T) {
	defer cleanDB(t)

	d := createTestDomain(t)

	res, err := sendWithUnsubscribe(t, d, "https://sender.example/unsub?email={{ email }}",
		&types.Recipient{Email: "first@email.com"},
		&types.Recipient{Email: "second@email.com"},
	)
	require.NoError(t, err)

	assert.EqualValues(t, 2, res.AcceptedCount)
	assert.Empty(t, res.RejectedRecipients)
	assert.ElementsMatch(t, []string{"first@email.com", "second@email.com"},
		poolEmails(t, res.MessageId))
}

// TestSendRejectsOnlyTheRecipientsThatCannotResolveTheUnsubscribe is the ADR
// 0005 intake rule: one row missing a field does not fail a send of thousands,
// and the caller is told which rows and why.
func TestSendRejectsOnlyTheRecipientsThatCannotResolveTheUnsubscribe(t *testing.T) {
	defer cleanDB(t)

	d := createTestDomain(t)

	res, err := sendWithUnsubscribe(t, d, "https://sender.example/unsub?t={{ token }}",
		&types.Recipient{Email: "has@email.com", Fields: map[string]string{"token": "abc"}},
		&types.Recipient{Email: "missing@email.com"},
	)
	require.NoError(t, err)

	assert.EqualValues(t, 1, res.AcceptedCount)
	assert.Equal(t, map[string]string{
		"missing@email.com": "unsubscribe_url_unresolved",
	}, rejections(t, res))
	assert.Equal(t, []string{"has@email.com"}, poolEmails(t, res.MessageId))
}

// TestSendFailsEntirelyOnAMalformedUnsubscribeURL separates the two levels of
// validation: a bad template is a fault in the request, not in one of its rows.
func TestSendFailsEntirelyOnAMalformedUnsubscribeURL(t *testing.T) {
	defer cleanDB(t)

	d := createTestDomain(t)

	for _, urlTemplate := range []string{
		"http://sender.example/unsub",
		"mailto:unsub@sender.example",
		"/unsub",
	} {
		_, err := sendWithUnsubscribe(t, d, urlTemplate, &types.Recipient{Email: "first@email.com"})

		require.Error(t, err, "expected %q to fail the call", urlTemplate)
		assert.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err))
	}
}

// TestSendWithoutAnUnsubscribeIsUnaffected pins the default: Kannon never adds
// an unsubscribe endpoint a caller did not state.
func TestSendWithoutAnUnsubscribeIsUnaffected(t *testing.T) {
	defer cleanDB(t)

	d := createTestDomain(t)

	res := requireSend(t, d, nil, &types.Recipient{Email: "first@email.com"})

	assert.EqualValues(t, 1, res.AcceptedCount)
	assert.Empty(t, res.RejectedRecipients)
}
