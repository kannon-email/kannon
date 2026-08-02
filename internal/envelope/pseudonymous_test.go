package envelope_test

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/kannon-email/kannon/internal/batch"
	"github.com/kannon-email/kannon/internal/envelope"
	"github.com/kannon-email/kannon/internal/tracking"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// pseudonymousPolicy tracks both axes under the rung that isolates a Recipient
// without naming them.
var pseudonymousPolicy = tracking.Policy{Opens: tracking.ModePseudonymous, Links: tracking.ModePseudonymous}

// mintCall is one request the Builder made of the token issuer.
type mintCall struct {
	messageID string
	identity  string
	url       string
	mode      tracking.Mode
}

// recordingTokens records the identity every mint was asked for. The tests below
// are about *what the Builder names*, which the opaque token that comes back
// cannot show — countingTokens answers the neighbouring question of whether a
// token was reused.
type recordingTokens struct {
	mu    sync.Mutex
	opens []mintCall
	links []mintCall
}

func (r *recordingTokens) CreateOpenToken(_ context.Context, messageID, identity string, mode tracking.Mode) (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.opens = append(r.opens, mintCall{messageID: messageID, identity: identity, mode: mode})
	return fmt.Sprintf("open-%d", len(r.opens)), nil
}

func (r *recordingTokens) CreateLinkToken(_ context.Context, messageID, identity, url string, mode tracking.Mode) (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.links = append(r.links, mintCall{messageID: messageID, identity: identity, url: url, mode: mode})
	return fmt.Sprintf("link-%d", len(r.links)), nil
}

// recordingBuilder is trackedBuilder over a token issuer that remembers what it
// was asked for.
func recordingBuilder(t *testing.T, rec *recordingTokens, links ...string) envelope.Builder {
	t.Helper()

	body := &strings.Builder{}
	body.WriteString("<html><body><h1>Hello!</h1>")
	for _, l := range links {
		fmt.Fprintf(body, "<a href=%q>x</a>", l)
	}
	body.WriteString("</body></html>")

	return envelope.NewBuilderWith(batchSource{data: envelope.SendingData{
		Subject:        "S",
		HTML:           body.String(),
		Domain:         "test.com",
		SenderEmail:    "noreply@test.com",
		SenderAlias:    "Test",
		DkimPrivateKey: newDKIMKeys(t),
	}}, rec)
}

func buildOne(t *testing.T, b envelope.Builder, batchID batch.ID, email string, p tracking.Policy) {
	t.Helper()
	_, err := b.Build(t.Context(), mustDeliveryTracked(t, batchID, email, nil, p))
	require.NoError(t, err)
}

// TestBuilderDrawsOnePseudonymPerDelivery is the definition of the Pseudonymous
// rung made observable: the pixel and *every* link of one Delivery are minted
// against the same identity, so the Recipient's engagement events are linkable to
// each other within the Batch. Drawing a pseudonym per token instead would leave
// every event a singleton — the tokens would look exactly as they do here, and the
// rung would silently record less than it promises.
func TestBuilderDrawsOnePseudonymPerDelivery(t *testing.T) {
	const first = "https://example.com/first"
	const second = "https://example.com/second"

	rec := &recordingTokens{}
	buildOne(t, recordingBuilder(t, rec, first, second), batch.ID("msg-1@test.com"), "a@example.com", pseudonymousPolicy)

	require.Len(t, rec.opens, 1)
	require.Len(t, rec.links, 2)

	pseudonym := rec.opens[0].identity
	for _, call := range rec.links {
		assert.Equal(t, pseudonym, call.identity,
			"the pixel and every link of one Delivery must carry the same pseudonym")
	}

	assert.NotEqual(t, "a@example.com", pseudonym, "no tracking URL may carry the recipient address")
	assert.True(t, tracking.InReservedNamespace(pseudonym, "test.com"),
		"a pseudonym must sit in the Domain's reserved namespace, got %q", pseudonym)
	assert.Equal(t, tracking.ModePseudonymous, rec.opens[0].mode)
}

// TestPseudonymsAreDrawnFreshEverywhere pins the other half of the rung: linkable
// *within* a Batch, and nowhere else. Two Recipients of one Batch must not collide,
// or their events would merge; and one Recipient across two Batches must not
// repeat, or the pseudonyms would compose into a durable identifier — which nobody,
// Kannon included, is able to resolve precisely because nothing derives them.
func TestPseudonymsAreDrawnFreshEverywhere(t *testing.T) {
	const landing = "https://example.com/landing"

	t.Run("TwoRecipientsOfOneBatch", func(t *testing.T) {
		rec := &recordingTokens{}
		b := recordingBuilder(t, rec, landing)
		batchID := batch.ID("msg-1@test.com")

		buildOne(t, b, batchID, "a@example.com", pseudonymousPolicy)
		buildOne(t, b, batchID, "b@example.com", pseudonymousPolicy)

		require.Len(t, rec.opens, 2)
		assert.NotEqual(t, rec.opens[0].identity, rec.opens[1].identity)
	})

	t.Run("OneRecipientInTwoBatches", func(t *testing.T) {
		rec := &recordingTokens{}
		b := recordingBuilder(t, rec, landing)

		buildOne(t, b, batch.ID("msg-1@test.com"), "a@example.com", pseudonymousPolicy)
		buildOne(t, b, batch.ID("msg-2@test.com"), "a@example.com", pseudonymousPolicy)

		require.Len(t, rec.opens, 2)
		assert.NotEqual(t, rec.opens[0].identity, rec.opens[1].identity)
	})
}

// TestPseudonymsAreDrawnOnlyWhenAnAxisAsksForOne keeps the two axes independent
// (ADR 0003) at the one place a Delivery now has two identities to choose between.
// A Delivery with one pseudonymous axis and one identified axis must not leak the
// pseudonym into the identified axis — which would lose the sender data they are
// entitled to — nor the address into the pseudonymous one, which is the whole
// point of the rung and is refused at the mint anyway.
func TestPseudonymsAreDrawnOnlyWhenAnAxisAsksForOne(t *testing.T) {
	const landing = "https://example.com/landing"

	t.Run("MixedAxes", func(t *testing.T) {
		rec := &recordingTokens{}
		buildOne(t, recordingBuilder(t, rec, landing), batch.ID("msg-1@test.com"), "a@example.com",
			tracking.Policy{Opens: tracking.ModePseudonymous, Links: tracking.ModeIdentified})

		require.Len(t, rec.opens, 1)
		require.Len(t, rec.links, 1)
		assert.True(t, tracking.InReservedNamespace(rec.opens[0].identity, "test.com"),
			"the pseudonymous axis carries a pseudonym")
		assert.Equal(t, "a@example.com", rec.links[0].identity,
			"the identified axis still names the Recipient")
	})

	t.Run("NoPseudonymousAxis", func(t *testing.T) {
		rec := &recordingTokens{}
		buildOne(t, recordingBuilder(t, rec, landing), batch.ID("msg-1@test.com"), "a@example.com", identifiedPolicy)

		require.Len(t, rec.opens, 1)
		require.Len(t, rec.links, 1)
		assert.Equal(t, "a@example.com", rec.opens[0].identity)
		assert.Equal(t, "a@example.com", rec.links[0].identity)
	})

	// The Builder hands the address over under Anonymous too and lets the mint
	// drop it, so that the decision lives at the one place every token passes
	// through rather than being made twice.
	t.Run("AnonymousLeavesTheDropToTheMint", func(t *testing.T) {
		rec := &recordingTokens{}
		buildOne(t, recordingBuilder(t, rec, landing), batch.ID("msg-1@test.com"), "a@example.com", anonymousPolicy)

		require.Len(t, rec.opens, 1)
		assert.Equal(t, "a@example.com", rec.opens[0].identity)
		assert.Equal(t, tracking.ModeAnonymous, rec.opens[0].mode)
	})
}

// TestBuilderDoesNotSharePseudonymousTokens is the cost side of the rung, and the
// bug it guards against is severe: the shared-token cache exists for the Modes
// whose token is a function of the Batch, and a pseudonymous token is a function
// of the Delivery. Handing a cached one to a second Recipient would file both
// Recipients' engagement under a single pseudonym.
func TestBuilderDoesNotSharePseudonymousTokens(t *testing.T) {
	const landing = "https://example.com/landing"

	b := trackedBuilder(t, landing)
	batchID := batch.ID("msg-1@test.com")

	one := deliverTo(t, b, batchID, "a@example.com", pseudonymousPolicy)
	two := deliverTo(t, b, batchID, "b@example.com", pseudonymousPolicy)

	assert.NotEqual(t, one.open, two.open, "a pseudonymous pixel is per-Delivery")
	assert.NotEqual(t, one.links, two.links, "a pseudonymous link is per-Delivery")
}
