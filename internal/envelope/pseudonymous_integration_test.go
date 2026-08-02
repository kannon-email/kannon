package envelope_test

import (
	"testing"

	"connectrpc.com/connect"
	"github.com/kannon-email/kannon/internal/batch"
	"github.com/kannon-email/kannon/internal/statssec"
	"github.com/kannon-email/kannon/internal/tracking"
	pb "github.com/kannon-email/kannon/proto/kannon/mailer/types"
	trackingtypes "github.com/kannon-email/kannon/proto/kannon/tracking/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/timestamppb"

	adminapiv1 "github.com/kannon-email/kannon/proto/kannon/admin/apiv1"
	mailerapiv1 "github.com/kannon-email/kannon/proto/kannon/mailer/apiv1"
)

// TestPseudonymousDeliveryEndToEnd walks the rung from the Mailer API call that
// states it to the tokens a recipient actually receives, through the real mint,
// because that is the only place its guarantees can be checked as a whole: the
// Builder decides what identity to draw, statssec decides what survives into the
// claim, and neither on its own proves what ends up in the URL.
//
// It pins the four things ADR 0006 promises an operator who states pseudonymous:
// no recipient address in any tracking URL, one pseudonym shared by the pixel and
// every link of a Delivery, and pseudonyms that collide neither between two
// Recipients of a Batch nor between two Batches of one Recipient.
func TestPseudonymousDeliveryEndToEnd(t *testing.T) {
	const fqdn = "pseudonymous-test.com"
	const first = "first@emailtest.com"
	const second = "second@emailtest.com"

	sender := newPseudonymousSender(t, fqdn)
	ss := statssec.NewStatsService(q)

	batchOne := sender.send(t, first, second)
	one := batchOne[first]
	two := batchOne[second]

	t.Run("NoTrackingURLNamesTheRecipient", func(t *testing.T) {
		// The claim is what a log-only observer reads, so it is asserted on the
		// verified claim rather than on the URL string alone; the URL is checked
		// too, since a JWT payload is readable base64 and the token *is* the URL.
		assert.NotContains(t, one.body, first)
		for _, token := range one.all() {
			claims := verifyAny(t, ss, token)
			assert.NotEqual(t, first, claims.identity)
			assert.True(t, tracking.InReservedNamespace(claims.identity, fqdn),
				"a pseudonymous token must name a sentinel of %q, got %q", fqdn, claims.identity)
			assert.Equal(t, tracking.ModePseudonymous, claims.mode)
		}
	})

	t.Run("ThePixelAndEveryLinkShareOnePseudonym", func(t *testing.T) {
		// This is what makes two engagement events of one Recipient linkable
		// inside the Batch, which is the whole definition of the rung.
		want := verifyAny(t, ss, one.open).identity
		require.NotEmpty(t, one.links)
		for _, token := range one.links {
			assert.Equal(t, want, verifyAny(t, ss, token).identity)
		}
	})

	t.Run("TwoRecipientsOfOneBatchAreNotLinkable", func(t *testing.T) {
		assert.NotEqual(t, verifyAny(t, ss, one.open).identity, verifyAny(t, ss, two.open).identity)
	})

	t.Run("OneRecipientInTwoBatchesIsNotLinkable", func(t *testing.T) {
		// Nothing derives a pseudonym, so this holds against Kannon itself and
		// not merely against an outside observer: there is no key that would let
		// the two be joined.
		batchTwo := sender.send(t, first)
		assert.NotEqual(t,
			verifyAny(t, ss, one.open).identity,
			verifyAny(t, ss, batchTwo[first].open).identity)
	})
}

// pseudonymousSender is an authenticated Domain that sends Batches stating
// pseudonymous on both axes.
type pseudonymousSender struct {
	domain *adminapiv1.Domain
	apiKey string
}

func newPseudonymousSender(t *testing.T, fqdn string) pseudonymousSender {
	t.Helper()

	d, err := adminAPI.CreateDomain(t.Context(), connect.NewRequest(&adminapiv1.CreateDomainRequest{Domain: fqdn}))
	require.NoError(t, err)

	key, err := adminAPI.CreateAPIKey(t.Context(), connect.NewRequest(&adminapiv1.CreateAPIKeyRequest{
		Domain: d.Msg.Domain,
		Name:   "pseudonymous-key",
	}))
	require.NoError(t, err)

	return pseudonymousSender{domain: d.Msg, apiKey: key.Msg.Key}
}

// send submits one Batch to the recipients and returns, per recipient, the
// tracking tokens their built message carries.
func (s pseudonymousSender) send(t *testing.T, recipients ...string) map[string]deliveredTokens {
	t.Helper()

	pseudonymous := &trackingtypes.TrackingPolicy{
		Opens: trackingtypes.TrackingMode_TRACKING_MODE_PSEUDONYMOUS,
		Links: trackingtypes.TrackingMode_TRACKING_MODE_PSEUDONYMOUS,
	}

	to := make([]*pb.Recipient, 0, len(recipients))
	for _, r := range recipients {
		to = append(to, &pb.Recipient{Email: r})
	}

	req := connect.NewRequest(&mailerapiv1.SendHTMLReq{
		Sender:        &pb.Sender{Email: "noreply@" + s.domain.Domain, Alias: "Test"},
		Subject:       "Pseudonymous",
		Html:          `<html><body><a href="https://example.com/a">a</a><a href="https://example.com/b">b</a></body></html>`,
		ScheduledTime: timestamppb.Now(),
		Recipients:    to,
		Tracking:      pseudonymous,
	})
	authRequest(req, s.domain, s.apiKey)

	res, err := ma.SendHTML(t.Context(), req)
	require.NoError(t, err)
	require.Empty(t, res.Msg.RejectedRecipients, "pseudonymous must be accepted at intake")

	out := make(map[string]deliveredTokens, len(recipients))
	for _, r := range recipients {
		claimed := markValidatedAndClaim(t, batch.ID(res.Msg.MessageId), r)
		require.Len(t, claimed, 1)
		require.Equal(t, r, claimed[0].Email(), "the claim must return the Delivery just validated")

		env, err := eb.Build(t.Context(), claimed[0])
		require.NoError(t, err)
		out[r] = readDeliveredTokens(t, env.Body())
	}
	return out
}

// verifiedClaims is the part of a verified token both channels share, so a test
// can assert on a Delivery's tokens without caring which endpoint each belongs to.
type verifiedClaims struct {
	identity string
	mode     tracking.Mode
}

// verifyAny verifies a token against whichever channel it was minted for, since a
// token is bound to its channel and the two are deliberately not interchangeable.
func verifyAny(t *testing.T, ss statssec.StatsService, token string) verifiedClaims {
	t.Helper()

	if open, err := ss.VerifyOpenToken(t.Context(), token); err == nil {
		return verifiedClaims{identity: open.Email, mode: open.Mode}
	}
	link, err := ss.VerifyLinkToken(t.Context(), token)
	require.NoError(t, err, "a token must verify on one of the two channels")
	return verifiedClaims{identity: link.Email, mode: link.Mode}
}
