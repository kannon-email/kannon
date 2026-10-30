package rest_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/kannon-email/kannon/internal/admintoken"
	"github.com/kannon-email/kannon/internal/authz"
	"github.com/kannon-email/kannon/internal/batch"
	sqlc "github.com/kannon-email/kannon/internal/db"
	"github.com/kannon-email/kannon/internal/domains"
	"github.com/kannon-email/kannon/internal/intake"
	"github.com/kannon-email/kannon/internal/values"
	"github.com/kannon-email/kannon/pkg/api/rest"
	"net/http"
	"net/http/httptest"
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
	service := intake.NewInlineService(sqlc.NewInlineStore(db), sqlc.NewDomainsRepository(db), nil, 0)
	h := rest.NewHandlerWithBatches(domains.NewService(sqlc.NewDomainsRepository(db)), admintoken.MustParse("secret"), db.Ping, service)
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
	for _, extra := range []string{`,"scheduled_at":"2099-01-01T00:00:00Z"`, `,"attachments":[]`, `,"global_fields":{}`, `,"tracking":{"opens":"off"}`} {
		call(path, "extension", strings.TrimSuffix(body, "}")+extra+"}", 400)
	}
	call(path, "", body, 400)
	call(path, "large", strings.Replace(body, "<p>Hello</p>", strings.Repeat("x", 10<<20), 1), 413)

	first := call(path, "retry", body, 201)
	if first["accepted_count"] != float64(1) || first["rejected_count"] != float64(2) {
		t.Fatal(first)
	}
	replay := call(path, "retry", body, 200)
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
	request := intake.InlineRequest{Sender: batch.Sender{Email: "sender@example.com"}, Subject: "Concurrent", HTML: "Hello", Recipients: []string{"one@example.com"}}
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
	maximum.Recipients = make([]string, 2000)
	for i := range maximum.Recipients {
		maximum.Recipients[i] = fmt.Sprintf("User.%d+tag@EXAMPLE.COM", i)
	}
	accepted, refused, _, err := service.Create(ctx, name, "maximum", maximum)
	if err != nil || accepted.AcceptedCount != 2000 || len(refused) != 0 {
		t.Fatal("2000 recipient boundary", err)
	}
	maximum.Recipients = []string{"User.Name+tag@example.com", "user.name+tag@example.com"}
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
