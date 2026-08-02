package envelope_test

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"mime/quotedprintable"
	"net/mail"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/kannon-email/kannon/internal/batch"
	"github.com/kannon-email/kannon/internal/delivery"
	"github.com/kannon-email/kannon/internal/dkim"
	"github.com/kannon-email/kannon/internal/envelope"
	"github.com/kannon-email/kannon/internal/tracking"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type stubSource struct {
	data envelope.SendingData
	err  error
}

func (s stubSource) GetSendingData(ctx context.Context, batchID batch.ID) (envelope.SendingData, error) {
	return s.data, s.err
}

type stubTokens struct {
	link, open string
}

func (s stubTokens) CreateLinkToken(ctx context.Context, messageID, email, url string, mode tracking.Mode) (string, error) {
	return s.link, nil
}
func (s stubTokens) CreateOpenToken(ctx context.Context, messageID, email string, mode tracking.Mode) (string, error) {
	return s.open, nil
}

// modeEchoTokens mints a token that spells out the Mode it was asked for, so a
// test can read off the rendered message which Mode was minted into which axis
// without reaching into the token issuer.
type modeEchoTokens struct{}

func (modeEchoTokens) CreateLinkToken(ctx context.Context, messageID, email, url string, mode tracking.Mode) (string, error) {
	return "link-" + string(mode), nil
}

func (modeEchoTokens) CreateOpenToken(ctx context.Context, messageID, email string, mode tracking.Mode) (string, error) {
	return "open-" + string(mode), nil
}

// batchSource renders every Batch from one template but reports each Batch's own
// id as its MessageID, the way the real query does, so a test can build two
// Batches through a single Builder.
type batchSource struct {
	data envelope.SendingData
}

func (s batchSource) GetSendingData(_ context.Context, batchID batch.ID) (envelope.SendingData, error) {
	data := s.data
	data.MessageID = batchID.String()
	return data, nil
}

// countingTokens mints a distinct token on every call, so a test can read off a
// delivered message whether the Builder asked for a fresh token or handed out one
// it had already issued.
type countingTokens struct {
	mu     sync.Mutex
	minted int
}

func (c *countingTokens) next(kind string) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.minted++
	return fmt.Sprintf("%s-%d", kind, c.minted), nil
}

func (c *countingTokens) CreateLinkToken(ctx context.Context, messageID, email, url string, mode tracking.Mode) (string, error) {
	return c.next("link")
}

func (c *countingTokens) CreateOpenToken(ctx context.Context, messageID, email string, mode tracking.Mode) (string, error) {
	return c.next("open")
}

// testBatchID is the Batch every single-Batch test builds against. Tests that
// care about the boundary between Batches state their own IDs and use
// mustDeliveryTracked directly.
const testBatchID = "msg-1@test.com"

// mustDelivery builds a Delivery carrying the Tracking Policy a fresh Domain
// resolves to, which is identified on both axes (ADR 0003).
func mustDelivery(t *testing.T, email string, fields map[string]string) *delivery.Delivery {
	t.Helper()
	return mustDeliveryTracked(t, batch.ID(testBatchID), email, fields, tracking.Policy{
		Opens: tracking.ModeIdentified,
		Links: tracking.ModeIdentified,
	})
}

// mustDeliveryTracked builds a Delivery whose frozen Tracking Policy is stated
// explicitly, as the Mailer API does at intake.
func mustDeliveryTracked(t *testing.T, batchID batch.ID, email string, fields map[string]string, p tracking.Policy) *delivery.Delivery {
	t.Helper()
	d, err := delivery.New(delivery.NewParams{
		BatchID:       batchID,
		Email:         email,
		Fields:        fields,
		Domain:        "test.com",
		ScheduledTime: time.Now(),
		Backoff:       delivery.DefaultBackoff,
		Tracking:      p,
	})
	assert.Nil(t, err)
	return d
}

func newDKIMKeys(t *testing.T) (privateKey string) {
	t.Helper()
	keys, err := dkim.GenerateDKIMKeysPair()
	assert.Nil(t, err)
	return keys.PrivateKey
}

func TestBuilderRendersSubjectFromAndTo(t *testing.T) {
	priv := newDKIMKeys(t)
	src := stubSource{data: envelope.SendingData{
		Subject:        "Hello {{ name }}",
		HTML:           "<html><body>hi {{name }}</body></html>",
		Domain:         "test.com",
		MessageID:      "msg-1",
		SenderEmail:    "noreply@test.com",
		SenderAlias:    "Test",
		DkimPrivateKey: priv,
	}}
	b := envelope.NewBuilderWith(src, stubTokens{link: "ltok", open: "otok"})

	d := mustDelivery(t, "rcpt@example.com", map[string]string{"name": "World"})
	env, err := b.Build(t.Context(), d)
	assert.Nil(t, err)

	assert.Equal(t, "rcpt@example.com", env.To())
	assert.Equal(t, "noreply@test.com", env.From())
	assert.True(t, env.ShouldRetry())

	parsed, err := mail.ReadMessage(bytes.NewReader(env.Body()))
	assert.Nil(t, err)
	assert.Equal(t, "Hello World", parsed.Header.Get("Subject"))
	assert.Equal(t, "Test <noreply@test.com>", parsed.Header.Get("From"))
	assert.Equal(t, "rcpt@example.com", parsed.Header.Get("To"))
}

func TestBuilderInsertsTrackingPixelAndRewritesLinks(t *testing.T) {
	priv := newDKIMKeys(t)
	src := stubSource{data: envelope.SendingData{
		Subject:        "S",
		HTML:           `<html><body><a href="https://example.com">x</a></body></html>`,
		Domain:         "test.com",
		MessageID:      "msg-1",
		SenderEmail:    "noreply@test.com",
		SenderAlias:    "Test",
		DkimPrivateKey: priv,
	}}
	b := envelope.NewBuilderWith(src, stubTokens{link: "LTOK", open: "OTOK"})

	d := mustDelivery(t, "rcpt@example.com", nil)
	env, err := b.Build(t.Context(), d)
	assert.Nil(t, err)

	parsed, err := mail.ReadMessage(bytes.NewReader(env.Body()))
	assert.Nil(t, err)
	bodyBytes, err := io.ReadAll(parsed.Body)
	assert.Nil(t, err)
	decoded, err := decodeQuotedPrintable(bodyBytes)
	assert.Nil(t, err)
	assert.True(t, strings.Contains(decoded, "https://stats.test.com/c/LTOK"), "click link missing in %q", decoded)
	assert.True(t, strings.Contains(decoded, "https://stats.test.com/o/OTOK"), "open pixel missing in %q", decoded)
}

func TestBuilderHonoursFrozenTrackingPolicy(t *testing.T) {
	const authoredLink = "https://example.com/landing"
	priv := newDKIMKeys(t)
	src := stubSource{data: envelope.SendingData{
		Subject:        "S",
		HTML:           fmt.Sprintf(`<html><body><a href=%q>x</a></body></html>`, authoredLink),
		Domain:         "test.com",
		MessageID:      "msg-1",
		SenderEmail:    "noreply@test.com",
		SenderAlias:    "Test",
		DkimPrivateKey: priv,
	}}
	b := envelope.NewBuilderWith(src, stubTokens{link: "LTOK", open: "OTOK"})

	cases := []struct {
		name          string
		policy        tracking.Policy
		wantPixel     bool
		wantRewritten bool
	}{
		{
			name:          "BothIdentified",
			policy:        tracking.Policy{Opens: tracking.ModeIdentified, Links: tracking.ModeIdentified},
			wantPixel:     true,
			wantRewritten: true,
		},
		{
			name:          "OpensOffLinksTracked",
			policy:        tracking.Policy{Opens: tracking.ModeOff, Links: tracking.ModeIdentified},
			wantPixel:     false,
			wantRewritten: true,
		},
		{
			name:          "LinksOffOpensTracked",
			policy:        tracking.Policy{Opens: tracking.ModeIdentified, Links: tracking.ModeOff},
			wantPixel:     true,
			wantRewritten: false,
		},
		{
			name:          "BothOff",
			policy:        tracking.Policy{Opens: tracking.ModeOff, Links: tracking.ModeOff},
			wantPixel:     false,
			wantRewritten: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d := mustDeliveryTracked(t, batch.ID("msg-1@test.com"), "rcpt@example.com", nil, tc.policy)
			env, err := b.Build(t.Context(), d)
			assert.Nil(t, err)

			parsed, err := mail.ReadMessage(bytes.NewReader(env.Body()))
			assert.Nil(t, err)
			bodyBytes, err := io.ReadAll(parsed.Body)
			assert.Nil(t, err)
			decoded, err := decodeQuotedPrintable(bodyBytes)
			assert.Nil(t, err)

			assert.Equal(t, tc.wantPixel, strings.Contains(decoded, "https://stats.test.com/o/OTOK"),
				"open pixel presence mismatch in %q", decoded)
			assert.Equal(t, tc.wantRewritten, strings.Contains(decoded, "https://stats.test.com/c/LTOK"),
				"rewritten link presence mismatch in %q", decoded)
			if !tc.wantRewritten {
				assert.True(t, strings.Contains(decoded, authoredLink),
					"an untracked link must be delivered as authored, got %q", decoded)
			}
			if !tc.wantPixel && !tc.wantRewritten {
				assert.False(t, strings.Contains(decoded, "stats.test.com"),
					"an untracked Delivery must carry no tracking hostname, got %q", decoded)
			}
		})
	}
}

// TestBuilderHonoursThePerLinkOptOut is the delivered-message half of the
// opt-out: the tracked link is rewritten, the opted-out one carries the href its
// author wrote, and the attribute that asked for that reaches nobody. A links
// Mode of Off is the case worth pinning, because under it no link is rewritten
// at all, so nothing on the rewriting path would have removed the attribute.
func TestBuilderHonoursThePerLinkOptOut(t *testing.T) {
	const (
		trackedLink  = "https://example.com/promo"
		optedOutLink = "https://example.com/preferences"
	)
	priv := newDKIMKeys(t)
	src := stubSource{data: envelope.SendingData{
		Subject: "S",
		HTML: fmt.Sprintf(
			`<html><body><a href=%q>promo</a><a href=%q data-no-track>preferences</a></body></html>`,
			trackedLink, optedOutLink),
		Domain:         "test.com",
		MessageID:      "msg-1",
		SenderEmail:    "noreply@test.com",
		SenderAlias:    "Test",
		DkimPrivateKey: priv,
	}}
	b := envelope.NewBuilderWith(src, stubTokens{link: "LTOK", open: "OTOK"})

	cases := []struct {
		name          string
		links         tracking.Mode
		wantRewritten bool
	}{
		{name: "LinksTracked", links: tracking.ModeIdentified, wantRewritten: true},
		{name: "LinksOff", links: tracking.ModeOff, wantRewritten: false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			policy := tracking.Policy{Opens: tracking.ModeIdentified, Links: tc.links}
			d := mustDeliveryTracked(t, batch.ID("msg-1@test.com"), "rcpt@example.com", nil, policy)
			env, err := b.Build(t.Context(), d)
			assert.Nil(t, err)

			parsed, err := mail.ReadMessage(bytes.NewReader(env.Body()))
			assert.Nil(t, err)
			bodyBytes, err := io.ReadAll(parsed.Body)
			assert.Nil(t, err)
			decoded, err := decodeQuotedPrintable(bodyBytes)
			assert.Nil(t, err)

			assert.False(t, strings.Contains(decoded, "data-no-track"),
				"the opt-out attribute must not reach the recipient, got %q", decoded)
			assert.True(t, strings.Contains(decoded, fmt.Sprintf("href=%q", optedOutLink)),
				"an opted-out link must be delivered as authored, got %q", decoded)
			assert.Equal(t, tc.wantRewritten, strings.Contains(decoded, "https://stats.test.com/c/LTOK"),
				"rewritten link presence mismatch in %q", decoded)
		})
	}
}

// TestBuilderMintsTokensCarryingTheFrozenMode pins the per-axis wiring: the opens
// Mode governs the pixel token and the links Mode governs the link token, taken
// from the Policy frozen on the Delivery. The two axes are independent, so the
// case that matters is the one where they differ.
func TestBuilderMintsTokensCarryingTheFrozenMode(t *testing.T) {
	priv := newDKIMKeys(t)
	src := stubSource{data: envelope.SendingData{
		Subject:        "S",
		HTML:           `<html><body><a href="https://example.com/landing">x</a></body></html>`,
		Domain:         "test.com",
		MessageID:      "msg-1",
		SenderEmail:    "noreply@test.com",
		SenderAlias:    "Test",
		DkimPrivateKey: priv,
	}}
	b := envelope.NewBuilderWith(src, modeEchoTokens{})

	d := mustDeliveryTracked(t, batch.ID("msg-1@test.com"), "rcpt@example.com", nil, tracking.Policy{
		Opens: tracking.ModeFull,
		Links: tracking.ModeIdentified,
	})
	env, err := b.Build(t.Context(), d)
	assert.Nil(t, err)

	parsed, err := mail.ReadMessage(bytes.NewReader(env.Body()))
	assert.Nil(t, err)
	bodyBytes, err := io.ReadAll(parsed.Body)
	assert.Nil(t, err)
	decoded, err := decodeQuotedPrintable(bodyBytes)
	assert.Nil(t, err)

	assert.True(t, strings.Contains(decoded, "https://stats.test.com/o/open-full"),
		"the open token must carry the opens Mode, got %q", decoded)
	assert.True(t, strings.Contains(decoded, "https://stats.test.com/c/link-identified"),
		"the link token must carry the links Mode, got %q", decoded)
}

// deliveredTokens are the tracking tokens one rendered message carries, read back
// out of the delivered body — the same place a recipient's mail client reads them.
// The decoded body comes along, for the assertions that are about what is *not*
// in it.
type deliveredTokens struct {
	body  string
	open  string
	links []string
}

// all is every token the message carries, for an assertion that holds of each
// regardless of which endpoint it belongs to.
func (d deliveredTokens) all() []string { return append([]string{d.open}, d.links...) }

var (
	pixelTokenRe = regexp.MustCompile(`/o/([A-Za-z0-9._-]+)`)
	linkTokenRe  = regexp.MustCompile(`/c/([A-Za-z0-9._-]+)`)
)

// deliverTo builds one Delivery of a Batch and returns the tracking tokens the
// recipient ends up holding.
func deliverTo(t *testing.T, b envelope.Builder, batchID batch.ID, email string, p tracking.Policy) deliveredTokens {
	t.Helper()

	env, err := b.Build(t.Context(), mustDeliveryTracked(t, batchID, email, nil, p))
	require.NoError(t, err)
	return readDeliveredTokens(t, env.Body())
}

// readDeliveredTokens reads the tracking tokens out of one built message.
func readDeliveredTokens(t *testing.T, raw []byte) deliveredTokens {
	t.Helper()

	parsed, err := mail.ReadMessage(bytes.NewReader(raw))
	require.NoError(t, err)
	body, err := io.ReadAll(parsed.Body)
	require.NoError(t, err)
	decoded, err := decodeQuotedPrintable(body)
	require.NoError(t, err)

	pixels := pixelTokenRe.FindAllStringSubmatch(decoded, -1)
	require.Len(t, pixels, 1, "a tracked message carries exactly one pixel, got %q", decoded)

	out := deliveredTokens{body: decoded, open: pixels[0][1]}
	for _, m := range linkTokenRe.FindAllStringSubmatch(decoded, -1) {
		out.links = append(out.links, m[1])
	}
	return out
}

// anonymousPolicy tracks both axes without naming anybody.
var anonymousPolicy = tracking.Policy{Opens: tracking.ModeAnonymous, Links: tracking.ModeAnonymous}

// identifiedPolicy tracks both axes and attributes to the Recipient.
var identifiedPolicy = tracking.Policy{Opens: tracking.ModeIdentified, Links: tracking.ModeIdentified}

// trackedBuilder returns a Builder over a Batch whose body carries the given
// links, minting a distinct token per request so reuse is visible in the output.
func trackedBuilder(t *testing.T, links ...string) envelope.Builder {
	t.Helper()
	return trackedBuilderWith(t, &countingTokens{}, links...)
}

// trackedBuilderWith is trackedBuilder over a stated token issuer, for the tests
// that read what the Builder asked for rather than what came back.
func trackedBuilderWith(t *testing.T, tokens envelope.TokenIssuer, links ...string) envelope.Builder {
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
	}}, tokens)
}

// TestBuilderSharesAnonymousTokensAcrossABatch is the Anonymous privacy property
// as a recipient can observe it: the token names nobody, so the two Recipients of
// a Batch must receive the *same* token and not merely two that look alike. Two
// independent mints would carry different iat/exp and so be distinguishable, which
// would let the two Recipients be told apart by the very token that was supposed
// to make them indistinguishable.
//
// It is also where the cost of Anonymous collapses: one signature per Batch, and
// one per Batch and URL, instead of one per link per Delivery.
func TestBuilderSharesAnonymousTokensAcrossABatch(t *testing.T) {
	const first = "https://example.com/first"
	const second = "https://example.com/second"

	b := trackedBuilder(t, first, second)
	batchID := batch.ID("msg-1@test.com")

	one := deliverTo(t, b, batchID, "a@example.com", anonymousPolicy)
	two := deliverTo(t, b, batchID, "b@example.com", anonymousPolicy)

	assert.Equal(t, one.open, two.open,
		"an anonymous pixel names nobody, so both Recipients must hold the same token")
	assert.Equal(t, one.links, two.links,
		"an anonymous link names nobody, so both Recipients must hold the same tokens")

	// The URL is part of what a link token commits to, so the two links of the
	// body must not collapse into one token.
	require.Len(t, one.links, 2)
	assert.NotEqual(t, one.links[0], one.links[1],
		"two different tracked URLs must get two different tokens")
	assert.NotEqual(t, one.open, one.links[0],
		"the pixel and the links are separate axes and must not share a token")
}

// TestBuilderDoesNotShareTokensBeyondTheirBatch is the negative half: reuse must
// stop at every boundary the token commits to. A token escaping its Batch would
// attribute one Domain's engagement to another's, and one escaping its Mode would
// hand a Recipient the wrong Policy over their own Delivery.
func TestBuilderDoesNotShareTokensBeyondTheirBatch(t *testing.T) {
	const landing = "https://example.com/landing"

	t.Run("DifferentBatches", func(t *testing.T) {
		b := trackedBuilder(t, landing)

		one := deliverTo(t, b, batch.ID("msg-1@test.com"), "a@example.com", anonymousPolicy)
		two := deliverTo(t, b, batch.ID("msg-2@test.com"), "a@example.com", anonymousPolicy)

		assert.NotEqual(t, one.open, two.open, "a pixel token must not leave its Batch")
		assert.NotEqual(t, one.links, two.links, "a link token must not leave its Batch")
	})

	t.Run("DifferentModes", func(t *testing.T) {
		b := trackedBuilder(t, landing)
		batchID := batch.ID("msg-1@test.com")

		anonymous := deliverTo(t, b, batchID, "a@example.com", anonymousPolicy)
		identified := deliverTo(t, b, batchID, "a@example.com", identifiedPolicy)

		assert.NotEqual(t, anonymous.open, identified.open, "a pixel token must not leave its Mode")
		assert.NotEqual(t, anonymous.links, identified.links, "a link token must not leave its Mode")
	})

	t.Run("IdentifiedNamesEachRecipient", func(t *testing.T) {
		b := trackedBuilder(t, landing)
		batchID := batch.ID("msg-1@test.com")

		one := deliverTo(t, b, batchID, "a@example.com", identifiedPolicy)
		two := deliverTo(t, b, batchID, "b@example.com", identifiedPolicy)

		// An identified token commits to the Recipient, so sharing it would
		// misattribute every engagement to whoever the Batch happened to build
		// first.
		assert.NotEqual(t, one.open, two.open, "an identified pixel is per-Delivery")
		assert.NotEqual(t, one.links, two.links, "an identified link is per-Delivery")
	})
}

func decodeQuotedPrintable(b []byte) (string, error) {
	r := quotedprintable.NewReader(bytes.NewReader(b))
	out, err := io.ReadAll(r)
	if err != nil {
		return "", err
	}
	return string(out), nil
}

func TestBuilderShouldRetryFalseAfterMaxAttempts(t *testing.T) {
	priv := newDKIMKeys(t)
	src := stubSource{data: envelope.SendingData{
		HTML:           "<html><body>x</body></html>",
		Domain:         "test.com",
		MessageID:      "msg-1",
		SenderEmail:    "noreply@test.com",
		SenderAlias:    "Test",
		DkimPrivateKey: priv,
	}}
	b := envelope.NewBuilderWith(src, stubTokens{})

	d := delivery.Load(delivery.LoadParams{
		BatchID:      batch.ID("msg-1@test.com"),
		Email:        "rcpt@example.com",
		Domain:       "test.com",
		SendAttempts: 10,
		Backoff:      delivery.DefaultBackoff,
	})
	env, err := b.Build(t.Context(), d)
	assert.Nil(t, err)
	assert.False(t, env.ShouldRetry())
}

func TestEnvelopeToProto(t *testing.T) {
	env := envelope.New(envelope.Params{
		EmailID:     "id",
		From:        "f@x",
		To:          "t@x",
		ReturnPath:  "rp",
		Body:        []byte("body"),
		ShouldRetry: true,
	})
	pb := env.ToProto()
	assert.Equal(t, "id", pb.EmailId)
	assert.Equal(t, "f@x", pb.From)
	assert.Equal(t, "t@x", pb.To)
	assert.Equal(t, "rp", pb.ReturnPath)
	assert.Equal(t, []byte("body"), pb.Body)
	assert.True(t, pb.ShouldRetry)
}

// TestBuilderCarriesTheUnsubscribeEndpoint checks the whole path: the Batch's
// template is personalised for this Delivery, percent-encoded, and emitted as
// the RFC 8058 pair.
func TestBuilderCarriesTheUnsubscribeEndpoint(t *testing.T) {
	priv := newDKIMKeys(t)
	src := stubSource{data: envelope.SendingData{
		Subject:        "Hello",
		HTML:           "<html><body>hi</body></html>",
		Domain:         "test.com",
		MessageID:      "msg-1",
		SenderEmail:    "noreply@test.com",
		SenderAlias:    "Test",
		DkimPrivateKey: priv,
		OneClickUnsubscribe: batch.OneClickUnsubscribe{
			URLTemplate: "https://sender.example/unsub?email={{ email }}",
		},
	}}
	b := envelope.NewBuilderWith(src, stubTokens{link: "ltok", open: "otok"})

	d := mustDelivery(t, "mario+rossi@example.com", nil)
	env, err := b.Build(t.Context(), d)
	require.NoError(t, err)

	parsed, err := mail.ReadMessage(bytes.NewReader(env.Body()))
	require.NoError(t, err)

	// The address is injected as the `email` field and escaped for a query, so
	// the '+' survives as %2B rather than reaching the endpoint as a space.
	assert.Equal(t, "<https://sender.example/unsub?email=mario%2Brossi%40example.com>",
		parsed.Header.Get("List-Unsubscribe"))
	assert.Equal(t, "List-Unsubscribe=One-Click", parsed.Header.Get("List-Unsubscribe-Post"))
}

// TestBuilderSignsAFixedHeaderSet pins the h= tag. It is fixed rather than
// derived from the message so that a header added in transit breaks the
// signature (RFC 6376 §5.4), and the two List-* names appear twice so that the
// protection extends to a message that already carries them.
func TestBuilderSignsAFixedHeaderSet(t *testing.T) {
	priv := newDKIMKeys(t)
	src := stubSource{data: envelope.SendingData{
		Subject:        "Hello",
		HTML:           "<html><body>hi</body></html>",
		Domain:         "test.com",
		MessageID:      "msg-1",
		SenderEmail:    "noreply@test.com",
		SenderAlias:    "Test",
		DkimPrivateKey: priv,
	}}
	b := envelope.NewBuilderWith(src, stubTokens{link: "ltok", open: "otok"})

	// No Cc and no unsubscribe on this message: they must still be signed.
	d := mustDelivery(t, "rcpt@example.com", nil)
	env, err := b.Build(t.Context(), d)
	require.NoError(t, err)

	parsed, err := mail.ReadMessage(bytes.NewReader(env.Body()))
	require.NoError(t, err)

	sig := parsed.Header.Get("DKIM-Signature")
	require.NotEmpty(t, sig)
	// Anchored on the tag separator, so this does not match the bh= body hash.
	m := regexp.MustCompile(`(?:^|;)\s*h=([^;]+)`).FindStringSubmatch(sig)
	require.Len(t, m, 2, "DKIM-Signature must carry an h= tag")
	signed := strings.Split(strings.ReplaceAll(m[1], " ", ""), ":")

	assert.Equal(t, []string{
		"From", "To", "Cc", "Subject", "Message-ID",
		"List-Unsubscribe", "List-Unsubscribe",
		"List-Unsubscribe-Post", "List-Unsubscribe-Post",
	}, signed)
}
