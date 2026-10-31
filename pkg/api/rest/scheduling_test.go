package rest_test

import (
	"encoding/base64"
	"errors"
	msgauth "github.com/emersion/go-msgauth/dkim"
	"github.com/kannon-email/kannon/internal/delivery"
	"github.com/kannon-email/kannon/internal/envelope"
	"github.com/kannon-email/kannon/internal/statssec"
	"github.com/stretchr/testify/require"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/mail"
	"strings"
	"testing"
	"time"

	"github.com/kannon-email/kannon/internal/admintoken"
	"github.com/kannon-email/kannon/internal/authz"
	"github.com/kannon-email/kannon/internal/batch"
	sqlc "github.com/kannon-email/kannon/internal/db"
	"github.com/kannon-email/kannon/internal/domains"
	"github.com/kannon-email/kannon/internal/intake"
	"github.com/kannon-email/kannon/internal/values"
	"github.com/kannon-email/kannon/pkg/api/rest"
)

func TestScheduledAttachments(t *testing.T) {
	db, cleanup, err := credentialDB()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	defer func() {
		if err := cleanup(); err != nil {
			t.Error(err)
		}
	}()
	now := time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)
	service := intake.NewInlineService(sqlc.NewInlineStore(db), sqlc.NewDomainsRepository(db), nil, 0)
	service.Now = func() time.Time { return now }
	h := rest.NewHandler(rest.Services{Domains: domains.NewService(sqlc.NewDomainsRepository(db)), Token: admintoken.MustParse("secret"), Ready: db.Ping, Inline: service})
	call := func(path, key, body string, status int) {
		t.Helper()
		r := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
		r.Header.Set("Authorization", "Bearer secret")
		r.Header.Set("Idempotency-Key", key)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != status {
			t.Fatalf("got %d want %d: %s", w.Code, status, w.Body)
		}
	}
	call("/v1beta1/domains", "", `{"domain":"example.com"}`, 201)
	domain := values.MustParse("example.com")
	ctx := authz.NewContext(t.Context(), authz.MustNewPrincipal("admin", authz.MustNewGrant(authz.RoleAdmin, authz.DomainAnchor(domain))))
	due := now.Add(time.Hour)
	p := intake.InlineRequest{Sender: batch.Sender{Email: "sender@example.com"}, Subject: "hello", HTML: "hello", Recipients: []intake.InlineRecipient{{Email: "to@example.com"}}, ScheduledAt: &due, Attachments: []intake.Attachment{{Filename: "hello.txt", Content: []byte("hello attachment")}}}
	receipt, _, _, err := service.Create(ctx, domain, "scheduled", p)
	if err != nil {
		t.Fatal(err)
	}
	if !receipt.ScheduledAt.Equal(due) || len(receipt.Attachments) != 1 || receipt.Attachments[0].SizeBytes != 16 {
		t.Fatal(receipt)
	}
	stored, err := sqlc.NewBatchRepository(db).GetByID(ctx, batch.ID(receipt.ID))
	if err != nil {
		t.Fatal(err)
	}
	if string(stored.Attachments()["hello.txt"]) != "hello attachment" {
		t.Fatal("attachment changed")
	}

	accepted, err := sqlc.NewDeliveryRepository(db, delivery.DefaultBackoff, delivery.DefaultRetryWindow).Get(ctx, batch.ID(receipt.ID), "to@example.com")
	require.NoError(t, err)
	message, err := envelope.NewBuilder(sqlc.New(db), statssec.NewStatsService(sqlc.New(db)), testReturnPath).Build(ctx, accepted)
	require.NoError(t, err)
	keys, err := sqlc.NewDomainsRepository(db).FindByName(ctx, domain)
	require.NoError(t, err)
	signatures, err := msgauth.VerifyWithOptions(strings.NewReader(string(message.Body())), &msgauth.VerifyOptions{LookupTXT: func(string) ([]string, error) { return []string{"v=DKIM1; k=rsa; p=" + keys.DkimPublicKey()}, nil }})
	require.NoError(t, err)
	require.Len(t, signatures, 1)
	require.NoError(t, signatures[0].Err)
	parsed, err := mail.ReadMessage(strings.NewReader(string(message.Body())))
	require.NoError(t, err)
	media, params, err := mime.ParseMediaType(parsed.Header.Get("Content-Type"))
	require.NoError(t, err)
	require.Equal(t, "multipart/mixed", media)
	parts := multipart.NewReader(parsed.Body, params["boundary"])
	found := false
	for {
		part, err := parts.NextPart()
		if errors.Is(err, io.EOF) {
			break
		}
		require.NoError(t, err)
		if part.FileName() != "hello.txt" {
			continue
		}
		found = true
		var reader io.Reader = part
		if strings.EqualFold(part.Header.Get("Content-Transfer-Encoding"), "base64") {
			reader = base64.NewDecoder(base64.StdEncoding, part)
		}
		data, err := io.ReadAll(reader)
		require.NoError(t, err)
		require.Equal(t, "hello attachment", string(data))
	}
	require.True(t, found, "attachment missing from signed MIME")
	now = due.Add(time.Hour)
	replay, _, replayed, err := service.Create(ctx, domain, "scheduled", p)
	if err != nil || !replayed || replay.ID != receipt.ID || !replay.ScheduledAt.Equal(due) || len(replay.Attachments) != 1 {
		t.Fatal(replay, replayed, err)
	}
	path := "/v1beta1/domains/example.com/batches"
	body := `{"from":{"email":"sender@example.com"},"subject":"hello","content":{"type":"html","html":"hello"},"recipients":[{"email":"to@example.com"}]`
	call(path, "past", body+`,"scheduled_at":"2030-01-01T01:00:00Z"}`, 422)
	call(path, "past", body+`}`, 201)
	call(path, "duplicate", body+`,"attachments":[{"filename":"a","content_base64":"YQ=="},{"filename":"a","content_base64":"Yg=="}]}`, 422)
	call(path, "duplicate", body+`,"attachments":[{"filename":"a","content_base64":"YQ=="}]}`, 201)
	oversized := base64.StdEncoding.EncodeToString(make([]byte, 8<<20))
	call(path, "size", body+`,"attachments":[{"filename":"a","content_base64":"`+oversized+`"}]}`, 413)
	call(path, "size", body+`}`, 201)
	exact := body + `}`
	exact += strings.Repeat(" ", (10<<20)-len(exact))
	call(path, "boundary", exact, 201)
	call(path, "boundary-over", exact+" ", 413)
	call(path, "boundary-over", body+`}`, 201)
}
