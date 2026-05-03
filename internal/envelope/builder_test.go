package envelope_test

import (
	"bytes"
	"context"
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

func (s stubTokens) CreateLinkToken(ctx context.Context, messageID, email, url string) (string, error) {
	return s.link, nil
}
func (s stubTokens) CreateOpenToken(ctx context.Context, messageID, email string) (string, error) {
	return s.open, nil
}

func mustDelivery(t *testing.T, batchID batch.ID, email string, fields map[string]string) *delivery.Delivery {
	t.Helper()
	d, err := delivery.New(delivery.NewParams{
		BatchID:       batchID,
		Email:         email,
		Fields:        fields,
		Domain:        "test.com",
		ScheduledTime: time.Now(),
		Backoff:       delivery.DefaultBackoff,
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
	env, err := b.Build(context.Background(), d)
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
	env, err := b.Build(context.Background(), d)
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
	env, err := b.Build(context.Background(), d)
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
