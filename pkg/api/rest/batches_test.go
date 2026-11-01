package rest_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	msgauth "github.com/emersion/go-msgauth/dkim"
	"github.com/kannon-email/kannon/internal/admintoken"
	"github.com/kannon-email/kannon/internal/authz"
	"github.com/kannon-email/kannon/internal/batch"
	sqlc "github.com/kannon-email/kannon/internal/db"
	"github.com/kannon-email/kannon/internal/delivery"
	"github.com/kannon-email/kannon/internal/domains"
	"github.com/kannon-email/kannon/internal/envelope"
	"github.com/kannon-email/kannon/internal/intake"
	"github.com/kannon-email/kannon/internal/statssec"
	"github.com/kannon-email/kannon/internal/templates"
	"github.com/kannon-email/kannon/internal/tracking"
	"github.com/kannon-email/kannon/internal/values"
	"github.com/kannon-email/kannon/pkg/api/rest"
	"github.com/stretchr/testify/require"
	"io"
	"mime/quotedprintable"
	"net/http"
	"net/http/httptest"
	"net/mail"
	"regexp"
	"strings"
	"sync"
	"testing"
)

func TestInlineBatchRetry(t *testing.T) {
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
	service := intake.NewInlineService(sqlc.NewInlineStore(db), sqlc.NewDomainsRepository(db), nil, 0, 0)
	h := rest.NewHandler(rest.Services{Domains: domains.NewService(sqlc.NewDomainsRepository(db)), Token: admintoken.MustParse("secret"), Ready: db.Ping, Inline: service})
	call := func(path, key, body string, status int) map[string]any {
		t.Helper()
		r := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
		r.Header.Set("Authorization", "Bearer secret")
		r.Header.Set("Idempotency-Key", key)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != status {
			t.Fatalf("got %d: %s", w.Code, w.Body)
		}
		var v map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &v); err != nil {
			t.Fatal(err)
		}
		return v
	}
	call("/v1beta1/domains", "", `{"domain":"example.com"}`, 201)
	body := `{"from":{"email":"sender@example.com"},"subject":"Hello","content":{"type":"html","html":"<p>Hello</p>"},"recipients":[{"email":" User+tag@EXAMPLE.com "},{"email":"User+tag@example.com"},{"email":"refused-address"}]}`
	path := "/v1beta1/domains/example.com/batches"
	if got := call(path, "limit", strings.Replace(body, `[{"email":" User+tag@EXAMPLE.com "},{"email":"User+tag@example.com"},{"email":"refused-address"}]`, "["+strings.Repeat(`{"email":"same@example.com"},`, 2000)+`{"email":"last@example.com"}]`, 1), 422); got["code"] != "too_many_recipients" {
		t.Fatal(got)
	}
	for _, extra := range []string{`,"unknown":true`} {
		call(path, "extension", strings.TrimSuffix(body, "}")+extra+"}", 400)
	}
	call(path, "", body, 400)
	call(path, "large", strings.Replace(body, "<p>Hello</p>", strings.Repeat("x", 10<<20), 1), 413)

	for _, extra := range []string{`,"scheduled_at":null`, `,"headers":null`, `,"tracking":{"opens":null}`, `,"global_fields":{"name":null}`} {
		call(path, "null", strings.TrimSuffix(body, "}")+extra+"}", 422)
	}
	allRefused := call(path, "all-refused", strings.Replace(body, `[{"email":" User+tag@EXAMPLE.com "},{"email":"User+tag@example.com"},{"email":"refused-address"}]`, `[{"email":"invalid"},{"email":"also-invalid"}]`, 1), 422)
	fieldErrors, ok := allRefused["errors"].([]any)
	require.True(t, ok)
	require.Len(t, fieldErrors, 2)
	require.Equal(t, "/recipients/1/email", func() any { v, ok := fieldErrors[1].(map[string]any); require.True(t, ok); return v["pointer"] }())
	display := call(path, "display", strings.TrimSuffix(body, "}")+`,"headers":{"to":["Audience <audience@example.net>"],"cc":["Copy <copy@example.net>"]}}`, 201)
	readBatch := func(id string) map[string]any {
		t.Helper()
		r := httptest.NewRequest(http.MethodGet, path+"/"+id, nil)
		r.Header.Set("Authorization", "Bearer secret")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		require.Equal(t, 200, w.Code, w.Body.String())
		var out map[string]any
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &out))
		return out
	}
	detail := readBatch(fmt.Sprint(display["id"]))
	require.Equal(t, float64(1), detail["accepted_count"])
	require.Equal(t, []any{"Audience <audience@example.net>"}, func() any { v, ok := detail["headers"].(map[string]any); require.True(t, ok); return v["to"] }())
	require.Equal(t, map[string]any{"opens": "identified", "links": "identified"}, detail["tracking"])
	tracked := strings.TrimSuffix(body, "}") + `,"tracking":{"opens":"anonymous","links":"off"}}`
	trackingResult := call(path, "tracking", tracked, 201)
	deliveryRepo := sqlc.NewDeliveryRepository(db, delivery.DefaultBackoff, delivery.DefaultRetryWindow)
	frozenDelivery, err := deliveryRepo.Get(t.Context(), batch.ID(fmt.Sprint(trackingResult["id"])), "User+tag@example.com")
	require.NoError(t, err)
	require.Equal(t, tracking.Policy{Opens: tracking.ModeAnonymous, Links: tracking.ModeOff}, frozenDelivery.TrackingPolicy())

	unsubscribe := `{"from":{"email":"sender@example.com"},"subject":"Hello","content":{"type":"html","html":"<p>Hello</p>"},"tracking":{"opens":"off","links":"off"},"one_click_unsubscribe":{"url_template":"https://sender.example/unsub?email={{ email }}&token={{ token }}"},"global_fields":{"token":"default"},"recipients":[{"email":"ok+tag@example.com","fields":{"token":"a&b"}},{"email":"unsupported@example.com","tracking":{"opens":"future"}},{"email":"above@example.com","tracking":{"links":"full"}}]}`
	u := call(path, "unsubscribe", unsubscribe, 201)
	require.Equal(t, "https://sender.example/unsub?email={{ email }}&token={{ token }}", func() any {
		v, ok := readBatch(fmt.Sprint(u["id"]))["one_click_unsubscribe"].(map[string]any)
		require.True(t, ok)
		return v["url_template"]
	}())
	require.Equal(t, float64(1), u["accepted_count"])
	require.Equal(t, float64(2), u["rejected_count"])
	rows, ok := u["rejected_recipients"].([]any)
	require.True(t, ok)
	require.Equal(t, "unsupported_tracking_mode", func() any { v, ok := rows[0].(map[string]any); require.True(t, ok); return v["code"] }())
	require.Equal(t, "tracking_above_ceiling", func() any { v, ok := rows[1].(map[string]any); require.True(t, ok); return v["code"] }())
	ud, err := deliveryRepo.Get(t.Context(), batch.ID(fmt.Sprint(u["id"])), "ok+tag@example.com")
	require.NoError(t, err)
	message, err := envelope.NewBuilder(sqlc.New(db), nil, testReturnPath).Build(t.Context(), ud)
	require.NoError(t, err)
	parsed, err := mail.ReadMessage(strings.NewReader(string(message.Body())))
	require.NoError(t, err)
	require.Equal(t, "<https://sender.example/unsub?email=ok%2Btag%40example.com&token=a%26b>", parsed.Header.Get("List-Unsubscribe"))
	require.Equal(t, "List-Unsubscribe=One-Click", parsed.Header.Get("List-Unsubscribe-Post"))
	require.Contains(t, parsed.Header.Get("DKIM-Signature"), "List-Unsubscribe")
	domainKeys, err := sqlc.NewDomainsRepository(db).FindByName(t.Context(), values.MustParse("example.com"))
	require.NoError(t, err)
	verified, err := msgauth.VerifyWithOptions(strings.NewReader(string(message.Body())), &msgauth.VerifyOptions{LookupTXT: func(string) ([]string, error) {
		return []string{"v=DKIM1; k=rsa; p=" + domainKeys.DkimPublicKey()}, nil
	}})
	require.NoError(t, err)
	require.Len(t, verified, 1)
	require.NoError(t, verified[0].Err)
	_, hasReplyTo := parsed.Header["Reply-To"]
	require.False(t, hasReplyTo, "a Batch stating no reply_to emits no Reply-To")
	require.NotContains(t, readBatch(fmt.Sprint(u["id"])), "reply_to")

	replyTo := call(path, "reply-to", strings.TrimSuffix(body, "}")+`,"tracking":{"opens":"off","links":"off"},"reply_to":{"email":"maintainer@elsewhere.example","name":"The Maintainer"}}`, 201)
	require.Equal(t, map[string]any{"email": "maintainer@elsewhere.example", "name": "The Maintainer"}, readBatch(fmt.Sprint(replyTo["id"]))["reply_to"])
	rd, err := deliveryRepo.Get(t.Context(), batch.ID(fmt.Sprint(replyTo["id"])), "User+tag@example.com")
	require.NoError(t, err)
	replyMessage, err := envelope.NewBuilder(sqlc.New(db), nil, testReturnPath).Build(t.Context(), rd)
	require.NoError(t, err)
	replyParsed, err := mail.ReadMessage(strings.NewReader(string(replyMessage.Body())))
	require.NoError(t, err)
	require.Equal(t, `"The Maintainer" <maintainer@elsewhere.example>`, replyParsed.Header.Get("Reply-To"))
	require.Equal(t, "<sender@example.com>", replyParsed.Header.Get("From"))
	require.Contains(t, replyParsed.Header.Get("DKIM-Signature"), "Reply-To")
	replyVerified, err := msgauth.VerifyWithOptions(strings.NewReader(string(replyMessage.Body())), &msgauth.VerifyOptions{LookupTXT: func(string) ([]string, error) {
		return []string{"v=DKIM1; k=rsa; p=" + domainKeys.DkimPublicKey()}, nil
	}})
	require.NoError(t, err)
	require.Len(t, replyVerified, 1)
	require.NoError(t, replyVerified[0].Err, "a stated Reply-To must not break the signature")
	bare := call(path, "reply-to-bare", strings.TrimSuffix(body, "}")+`,"reply_to":{"email":"maintainer@elsewhere.example"}}`, 201)
	require.Equal(t, map[string]any{"email": "maintainer@elsewhere.example"}, readBatch(fmt.Sprint(bare["id"]))["reply_to"])
	call(path, "reply-to-unknown", strings.TrimSuffix(body, "}")+`,"reply_to":{"email":"maintainer@elsewhere.example","to":"x"}}`, 400)
	// An address that does not parse is well-formed JSON with an invalid value,
	// in reply_to and in from alike.
	require.Equal(t, "validation_failed", call(path, "from-invalid", strings.Replace(body, `"sender@example.com"`, `"not-an-address"`, 1), 422)["code"])
	for key, extra := range map[string]string{
		"reply-to-invalid":   `,"reply_to":{"email":"not-an-address"}}`,
		"reply-to-null":      `,"reply_to":null`,
		"reply-to-no-email":  `,"reply_to":{"name":"The Maintainer"}}`,
		"reply-to-empty":     `,"reply_to":{}}`,
		"reply-to-injection": `,"reply_to":{"email":"maintainer@elsewhere.example","name":"M\r\nBcc: victim@example.com"}}`,
	} {
		require.Equal(t, "validation_failed", call(path, key, strings.TrimSuffix(body, "}")+strings.TrimSuffix(extra, "}")+"}", 422)["code"], key)
	}
	// Exercise REST intake through actual signed engagement tokens, with no DNS/network calls.
	ss := statssec.NewStatsService(sqlc.New(db))
	for _, mode := range []string{"anonymous", "pseudonymous"} {
		privacyBody := strings.Replace(strings.Replace(tracked, "anonymous", mode, 1), "<p>Hello</p>", "<html><body>Hello</body></html>", 1)
		privacy := call(path, "privacy-"+mode, privacyBody, 201)
		pd, err := deliveryRepo.Get(t.Context(), batch.ID(fmt.Sprint(privacy["id"])), "User+tag@example.com")
		require.NoError(t, err)
		pm, err := envelope.NewBuilder(sqlc.New(db), ss, testReturnPath).Build(t.Context(), pd)
		require.NoError(t, err)
		pmail, err := mail.ReadMessage(strings.NewReader(string(pm.Body())))
		require.NoError(t, err)
		decoded, err := io.ReadAll(quotedprintable.NewReader(pmail.Body))
		require.NoError(t, err)
		token := regexp.MustCompile(`https://stats.example.com/o/([^" >]+)`).FindStringSubmatch(string(decoded))
		require.Len(t, token, 2)
		claims, err := ss.VerifyOpenToken(t.Context(), token[1])
		require.NoError(t, err)
		require.Equal(t, tracking.Mode(mode), claims.Mode)
		require.NotEqual(t, "User+tag@example.com", claims.Email)
		require.True(t, tracking.InReservedNamespace(claims.Email, "example.com"))
	}

	unresolved := strings.Replace(unsubscribe, "{{ token }}", "{{ missing }}", 1)
	require.Equal(t, "no_recipients_accepted", call(path, "unresolved", unresolved, 422)["code"])
	call(path, "bad-url", strings.Replace(unsubscribe, "https://sender.example", "http://sender.example", 1), 422)

	_, err = sqlc.NewDomainsRepository(db).SetTrackingPolicy(t.Context(), values.MustParse("example.com"), tracking.Policy{Opens: tracking.ModeOff, Links: tracking.ModeOff})
	require.NoError(t, err)
	// A successful replay must not reconsider the now stricter Domain ceiling.
	require.Equal(t, u["id"], call(path, "unsubscribe", unsubscribe, 200)["id"])
	require.Equal(t, trackingResult["id"], call(path, "tracking", tracked, 200)["id"])
	call(path, "tracking-new", tracked, 422)
	stillFrozen, err := deliveryRepo.Get(t.Context(), batch.ID(fmt.Sprint(trackingResult["id"])), "User+tag@example.com")
	require.NoError(t, err)
	require.Equal(t, tracking.ModeAnonymous, stillFrozen.TrackingPolicy().Opens)

	assertSend := func(id, email, subject, html string) {
		t.Helper()
		d, err := sqlc.NewDeliveryRepository(db, delivery.DefaultBackoff, delivery.DefaultRetryWindow).Get(t.Context(), batch.ID(id), email)
		require.NoError(t, err)
		e, err := envelope.NewBuilder(sqlc.New(db), nil, testReturnPath).Build(t.Context(), d)
		require.NoError(t, err)
		msg, err := mail.ReadMessage(strings.NewReader(string(e.Body())))
		require.NoError(t, err)
		require.Equal(t, subject, msg.Header.Get("Subject"))
		decoded, err := io.ReadAll(quotedprintable.NewReader(msg.Body))
		require.NoError(t, err)

		require.Contains(t, string(decoded), html)
	}
	personal := `{"from":{"email":"sender@example.com"},"subject":"Hello {{ name }}","content":{"type":"html","html":"<p>{{ name }} {{ email }}</p>"},"global_fields":{"name":"Default"},"recipients":[{"email":"one@example.com","fields":{"name":"$1\\literal"}},{"email":"two@example.com","fields":{"email":"override"}},{"email":"three@example.com","fields":{}}]}`
	got := call(path, "personal", personal, 201)
	if got["accepted_count"] != float64(2) || got["rejected_count"] != float64(1) {
		t.Fatal(got)
	}
	assertSend(fmt.Sprint(got["id"]), "one@example.com", `Hello $1\literal`, `<p>$1\literal one@example.com</p>`)
	assertSend(fmt.Sprint(got["id"]), "three@example.com", "Hello Default", "<p>Default three@example.com</p>")
	missing := strings.Replace(personal, `"global_fields":{"name":"Default"},`, "", 1)
	m := call(path, "missing", missing, 201)
	require.Equal(t, float64(1), m["accepted_count"])
	require.Equal(t, float64(2), m["rejected_count"])
	call(path, "reserved-global", strings.Replace(personal, `"name":"Default"`, `"email":"spoof"`, 1), 422)
	repo := sqlc.NewTemplatesRepository(db)
	source, err := templates.NewPersistent(values.MustParse("example.com"), "<p>{{ name }} {{ email }}</p>", "Original")
	require.NoError(t, err)
	require.NoError(t, repo.Create(t.Context(), source))
	senderCtx := authz.NewContext(t.Context(), authz.MustNewPrincipal("sender", authz.MustNewGrant(authz.RoleSender, authz.DomainAnchor(values.MustParse("example.com")))))
	senderRequest := intake.InlineRequest{Sender: batch.Sender{Email: "sender@example.com"}, Subject: "Hi {{ name }}", TemplateID: source.TemplateID(), GlobalFields: map[string]string{"name": "Sender"}, Recipients: []intake.InlineRecipient{{Email: "sender-only@example.com"}}}
	acceptedBySender, _, _, err := service.Create(senderCtx, values.MustParse("example.com"), "sender-template", senderRequest)
	require.NoError(t, err)
	assertSend(acceptedBySender.ID, "sender-only@example.com", "Hi Sender", "<p>Sender sender-only@example.com</p>")
	foreign, err := templates.NewPersistent(values.MustParse("other.example.com"), "Other", "Other")
	require.NoError(t, err)
	call("/v1beta1/domains", "", `{"domain":"other.example.com"}`, 201)
	require.NoError(t, repo.Create(t.Context(), foreign))
	senderRequest.TemplateID = foreign.TemplateID()
	_, err = func() (*intake.InlineReceipt, error) {
		r, _, _, e := service.Create(senderCtx, values.MustParse("example.com"), "foreign-template", senderRequest)
		return r, e
	}()
	require.ErrorIs(t, err, templates.ErrTemplateNotFound)
	templated := strings.Replace(personal, `"type":"html","html":"<p>{{ name }} {{ email }}</p>"`, `"type":"template","template_id":"`+source.TemplateID()+`"`, 1)
	frozen := call(path, "template", templated, 201)
	_, err = repo.Update(t.Context(), source.TemplateID(), func(tpl *templates.Template) error { tpl.SetHTML("CHANGED"); return nil })
	require.NoError(t, err)
	assertSend(fmt.Sprint(frozen["id"]), "one@example.com", `Hello $1\literal`, `<p>$1\literal one@example.com</p>`)
	_, err = repo.Delete(t.Context(), source.TemplateID())
	require.NoError(t, err)
	assertSend(fmt.Sprint(frozen["id"]), "one@example.com", `Hello $1\literal`, `<p>$1\literal one@example.com</p>`)
	again := call(path, "template", templated, 200)
	require.Equal(t, frozen["id"], again["id"])
	require.Equal(t, frozen["accepted_count"], again["accepted_count"])
	require.NotContains(t, again, "rejected_recipients")
	first := call(path, "retry", body, 201)
	if first["accepted_count"] != float64(1) || first["rejected_count"] != float64(2) {
		t.Fatal(first)
	}
	replay := call(path, "retry", body, 200)
	if replay["created_at"] != first["created_at"] {
		t.Fatal("creation time changed on replay", first, replay)
	}
	if replay["id"] != first["id"] {
		t.Fatal(replay)
	}
	if _, ok := replay["rejected_recipients"]; ok {
		t.Fatal("replay leaks refusals")
	}
	call(path, "retry", strings.Replace(body, "Hello", "Changed", 1), 409)
	rejected, ok := first["rejected_recipients"].([]any)
	if !ok || len(rejected) != 2 {
		t.Fatal(first)
	}
	refusal, ok := rejected[0].(map[string]any)
	if !ok {
		t.Fatal(rejected)
	}
	if refusal["index"] != float64(1) || refusal["code"] != "duplicate_recipient" {
		t.Fatal(rejected)
	}
	counts := func() string {
		t.Helper()
		var n string
		err := db.QueryRow(t.Context(), `SELECT concat((SELECT count(*) FROM messages),':',(SELECT count(*) FROM templates),':',(SELECT count(*) FROM sending_pool_emails),':',(SELECT count(*) FROM batch_intake_receipts),':',(SELECT count(*) FROM delivery_intake_facts))`).Scan(&n)
		if err != nil {
			t.Fatal(err)
		}
		return n
	}
	before := counts()
	allBad := strings.Replace(body, ` User+tag@EXAMPLE.com `, `bad`, 1)
	allBad = strings.Replace(allBad, `User+tag@example.com`, `also-bad`, 1)
	call(path, "correctable", allBad, 422)
	if counts() != before {
		t.Fatal("all refused persisted work")
	}
	call(path, "correctable", body, 201)
	before = counts()
	_, err = db.Exec(t.Context(), `CREATE FUNCTION refuse_inline() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'injected failure'; END $$; CREATE TRIGGER refuse_inline BEFORE INSERT ON delivery_intake_facts FOR EACH ROW EXECUTE FUNCTION refuse_inline()`)
	if err != nil {
		t.Fatal(err)
	}
	call(path, "rollback", body, 500)
	if counts() != before {
		t.Fatal("partial intake survived rollback")
	}
	_, err = db.Exec(t.Context(), `DROP TRIGGER refuse_inline ON delivery_intake_facts; DROP FUNCTION refuse_inline()`)
	if err != nil {
		t.Fatal(err)
	}
	call(path, "rollback", body, 201)
	// Simulate a connection lost while the committed response is being written.
	r := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	r.Header.Set("Authorization", "Bearer secret")
	r.Header.Set("Idempotency-Key", "lost")
	h.ServeHTTP(&lostWriter{header: http.Header{}}, r)
	before = counts()
	call(path, "lost", body, 200)
	if counts() != before {
		t.Fatal("lost response sent twice")
	}
	// The authorized application seam exercises simultaneous retries independently
	// of HTTP serialization and asserts one identity across all successful calls.
	name := values.MustParse("example.com")
	ctx := authz.NewContext(t.Context(), authz.MustNewPrincipal("sender", authz.MustNewGrant(authz.RoleSender, authz.DomainAnchor(name))))
	request := intake.InlineRequest{Sender: batch.Sender{Email: "sender@example.com"}, Subject: "Concurrent", HTML: "Hello", Recipients: []intake.InlineRecipient{{Email: "one@example.com"}}}
	var wg sync.WaitGroup
	ids := make(chan string, 16)
	errs := make(chan error, 16)
	start := make(chan struct{})
	for range 16 {
		wg.Go(func() {
			<-start
			receipt, _, _, err := service.Create(ctx, name, "concurrent", request)
			if err != nil {
				errs <- err
			} else {
				ids <- receipt.ID
			}
		})
	}
	close(start)
	wg.Wait()
	close(ids)
	close(errs)
	id := ""
	for next := range ids {
		if id != "" && id != next {
			t.Fatal("multiple concurrent identities")
		}
		id = next
	}
	if id == "" {
		t.Fatal("no successful creation")
	}
	for err := range errs {
		if !errors.Is(err, intake.ErrIdempotencyInProgress) {
			t.Fatal(err)
		}
	}
	maximum := request
	maximum.Recipients = make([]intake.InlineRecipient, 2000)
	for i := range maximum.Recipients {
		maximum.Recipients[i] = intake.InlineRecipient{Email: fmt.Sprintf("User.%d+tag@EXAMPLE.COM", i)}
	}
	accepted, refused, _, err := service.Create(ctx, name, "maximum", maximum)
	if err != nil || accepted.AcceptedCount != 2000 || len(refused) != 0 {
		t.Fatal("2000 recipient boundary", err)
	}
	maximum.Recipients = []intake.InlineRecipient{{Email: "User.Name+tag@example.com"}, {Email: "user.name+tag@example.com"}}
	accepted, _, _, err = service.Create(ctx, name, "local-case", maximum)
	if err != nil || accepted.AcceptedCount != 2 {
		t.Fatal("local case changed", err)
	}
	maximum.Sender.Email = "sender@unrelated.com"
	if _, _, _, err = service.Create(ctx, name, "spoof", maximum); !errors.Is(err, authz.ErrForbidden) {
		t.Fatal("sender constraint", err)
	}

	// An active transaction holding this intake key gives deterministic retry guidance.
	tx, err := db.Begin(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	_, err = tx.Exec(t.Context(), `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, "example.com:busy")
	if err != nil {
		t.Fatal(err)
	}
	call(path, "busy", body, 409)
	if err = tx.Rollback(t.Context()); err != nil {
		t.Fatal(err)
	}
	// No input refusal details are stored in any accepted-work table.
	for _, table := range []string{"batch_intake_receipts", "delivery_intake_facts", "messages", "sending_pool_emails"} {
		var leak bool
		err = db.QueryRow(t.Context(), fmt.Sprintf(`SELECT EXISTS(SELECT 1 FROM %s t WHERE row_to_json(t)::text LIKE '%%refused-address%%' OR row_to_json(t)::text LIKE '%%duplicate_recipient%%')`, table)).Scan(&leak)
		if err != nil || leak {
			t.Fatal("persisted refusal", table, err)
		}
	}
	// Replay still checks current authority.
	denied := authz.NewContext(t.Context(), authz.MustNewPrincipal("revoked"))
	if _, _, _, err = service.Create(denied, name, "concurrent", request); !errors.Is(err, authz.ErrForbidden) {
		t.Fatal("unauthorized replay", err)
	}
}

type lostWriter struct{ header http.Header }

func (w *lostWriter) Header() http.Header     { return w.header }
func (*lostWriter) WriteHeader(int)           {}
func (*lostWriter) Write([]byte) (int, error) { return 0, errors.New("connection lost") }
