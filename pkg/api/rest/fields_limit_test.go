package rest_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"testing"

	"github.com/kannon-email/kannon/internal/admintoken"
	sqlc "github.com/kannon-email/kannon/internal/db"
	"github.com/kannon-email/kannon/internal/domains"
	"github.com/kannon-email/kannon/internal/intake"
	"github.com/kannon-email/kannon/pkg/api/rest"
	"github.com/stretchr/testify/require"
)

type fieldsClient struct {
	t *testing.T
	h http.Handler
}

func newFieldsClient(t *testing.T) fieldsClient {
	t.Helper()
	db, cleanup, err := credentialDB()
	require.NoError(t, err)
	t.Cleanup(func() {
		db.Close()
		require.NoError(t, cleanup())
	})
	service := intake.NewInlineService(sqlc.NewInlineStore(db), sqlc.NewDomainsRepository(db), nil, 0, 0)
	c := fieldsClient{t: t, h: rest.NewHandler(rest.Services{Domains: domains.NewService(sqlc.NewDomainsRepository(db)), Token: admintoken.MustParse(testAdminToken), Ready: db.Ping, Inline: service})}
	c.do(http.MethodPost, "/v1/domains", "", `{"domain":"example.com"}`, 201)
	return c
}

func (c fieldsClient) do(method, path, key, body string, status int) map[string]any {
	c.t.Helper()
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	r.Header.Set("Authorization", "Bearer "+testAdminToken)
	if key != "" {
		r.Header.Set("Idempotency-Key", key)
	}
	w := httptest.NewRecorder()
	c.h.ServeHTTP(w, r)
	require.Equal(c.t, status, w.Code, w.Body.String())
	var v map[string]any
	require.NoError(c.t, json.Unmarshal(w.Body.Bytes(), &v))
	return v
}

func (c fieldsClient) send(key string, global map[string]string, recipients int) map[string]any {
	c.t.Helper()
	return c.sendExpecting(key, global, recipients, 0)
}

func (c fieldsClient) sendExpecting(key string, global map[string]string, recipients, status int) map[string]any {
	c.t.Helper()
	rs := make([]map[string]any, recipients)
	for i := range rs {
		rs[i] = map[string]any{"email": fmt.Sprintf("r%d@example.net", i), "fields": map[string]string{"name": fmt.Sprint("R", i)}}
	}
	raw, err := json.Marshal(map[string]any{
		"from":          map[string]string{"email": "sender@example.com"},
		"subject":       "Hello {{ name }}",
		"content":       map[string]string{"type": "html", "html": "<p>Hello {{ name }}</p>"},
		"global_fields": global,
		"recipients":    rs,
	})
	require.NoError(c.t, err)
	if status == 0 {
		status = 201
	}
	return c.do(http.MethodPost, "/v1/domains/example.com/batches", key, string(raw), status)
}

func (c fieldsClient) batchCount() int {
	c.t.Helper()
	page := c.do(http.MethodGet, "/v1/domains/example.com/batches", "", "", 200)
	items, ok := page["items"].([]any)
	require.True(c.t, ok, page)
	return len(items)
}

func requireLimitProblem(t *testing.T, got map[string]any, code, detail string) {
	t.Helper()
	require.Equal(t, code, got["code"])
	errs, ok := got["errors"].([]any)
	require.True(t, ok, got)
	require.Len(t, errs, 1)
	e, ok := errs[0].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "", e["pointer"])
	require.Contains(t, e["detail"], detail)
}

// The review's proof of concept: 50,000 empty global fields copied into 200 Recipients
// allocated about two gigabytes and wrote tens of megabytes of Delivery fields.
func TestBatchWithTooManyGlobalFieldsIsRefusedBeforeCopying(t *testing.T) {
	c := newFieldsClient(t)
	global := make(map[string]string, 50_000)
	for i := range 50_000 {
		global[fmt.Sprint("k", i)] = ""
	}
	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	got := c.sendExpecting("poc", global, 200, 422)
	runtime.ReadMemStats(&after)
	requireLimitProblem(t, got, "too_many_fields", "1000 fields")
	// Decoding the request alone allocates tens of megabytes; copying it per Recipient
	// allocated gigabytes. The margin absorbs what -race adds.
	require.Less(t, after.TotalAlloc-before.TotalAlloc, uint64(256<<20))
	require.Zero(t, c.batchCount())
}

func TestBatchWhoseMergedFieldsExceedTheByteBudgetIsRefused(t *testing.T) {
	c := newFieldsClient(t)
	global := make(map[string]string, 900)
	for i := range 900 {
		global[fmt.Sprintf("k%03d", i)] = strings.Repeat("v", 100)
	}
	// About 150 KB of global fields with their overhead, merged into 200 Recipients: about 30 MB.
	requireLimitProblem(t, c.sendExpecting("bytes", global, 200, 422), "fields_too_large", "10 MiB")
	require.Zero(t, c.batchCount())
	// The same fields for fewer Recipients stay within the budget.
	require.Equal(t, float64(50), c.send("fewer", global, 50)["accepted_count"])
}

func TestRecipientFieldsAreCountedWithTheGlobalOnes(t *testing.T) {
	c := newFieldsClient(t)
	global := make(map[string]string, 999)
	for i := range 999 {
		global[fmt.Sprintf("k%03d", i)] = "v"
	}
	// Each Recipient also states name: 1000 fields in all.
	require.Equal(t, float64(2), c.send("at-limit", global, 2)["accepted_count"])
	global["one-more"] = "v"
	requireLimitProblem(t, c.sendExpecting("over-limit", global, 2, 422), "too_many_fields", "1000 fields")
}

// Copying a field costs a map entry whatever its length, so many empty fields
// merged into many Recipients are refused even though their bytes are few.
func TestBatchOfManyEmptyFieldsTimesManyRecipientsIsRefused(t *testing.T) {
	c := newFieldsClient(t)
	global := make(map[string]string, 999)
	for i := range 999 {
		global[fmt.Sprintf("k%03d", i)] = ""
	}
	requireLimitProblem(t, c.sendExpecting("empty", global, 2000, 422), "fields_too_large", "10 MiB")
	require.Zero(t, c.batchCount())
}
