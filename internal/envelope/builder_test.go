package envelope_test

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"mime/quotedprintable"
	"net/mail"
	"strings"
	"testing"
	"time"

	"github.com/kannon-email/kannon/internal/batch"
	"github.com/kannon-email/kannon/internal/delivery"
	"github.com/kannon-email/kannon/internal/dkim"
	"github.com/kannon-email/kannon/internal/envelope"
	"github.com/kannon-email/kannon/internal/tracking"
	"github.com/stretchr/testify/assert"
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

// mustDelivery builds a Delivery carrying the Tracking Policy a fresh Domain
// resolves to, which is identified on both axes (ADR 0003).
func mustDelivery(t *testing.T, batchID batch.ID, email string, fields map[string]string) *delivery.Delivery {
	t.Helper()
	return mustDeliveryTracked(t, batchID, email, fields, tracking.Policy{
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

	d := mustDelivery(t, batch.ID("msg-1@test.com"), "rcpt@example.com", map[string]string{"name": "World"})
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

	d := mustDelivery(t, batch.ID("msg-1@test.com"), "rcpt@example.com", nil)
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
