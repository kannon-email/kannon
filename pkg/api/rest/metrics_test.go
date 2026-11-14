package rest_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/kannon-email/kannon/internal/admintoken"
	sqlc "github.com/kannon-email/kannon/internal/db"
	"github.com/kannon-email/kannon/internal/domains"
	"github.com/kannon-email/kannon/internal/intake"
	"github.com/kannon-email/kannon/internal/mailflow"
	"github.com/kannon-email/kannon/pkg/api/rest"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A replay persists nothing, so it counts nothing. A Batch whose every Recipient is refused is not
// persisted either, but the Recipients were decided at Intake and are counted as rejected.
func TestRESTIntakeCountsPersistedBatchesAndTheRecipientsItDecided(t *testing.T) {
	db, cleanup, err := credentialDB()
	require.NoError(t, err)
	t.Cleanup(func() {
		db.Close()
		assert.NoError(t, cleanup())
	})
	reg := prometheus.NewRegistry()
	h := rest.NewHandler(rest.Services{
		Domains: domains.NewService(sqlc.NewDomainsRepository(db)),
		Token:   admintoken.MustParse("secret"),
		Inline:  intake.NewInlineService(sqlc.NewInlineStore(db), sqlc.NewDomainsRepository(db), nil, 0, 0),
		Flow:    mailflow.New(reg),
	})
	call := func(path, key, body string, status int) {
		t.Helper()
		r := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
		r.Header.Set("Authorization", "Bearer secret")
		r.Header.Set("Idempotency-Key", key)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		require.Equal(t, status, w.Code, w.Body.String())
	}
	batch := func(recipients string) string {
		return `{"from":{"email":"sender@example.com"},"subject":"Hello","content":{"type":"html","html":"<p>Hello</p>"},"recipients":[` + recipients + `]}`
	}

	call("/v1/domains", "", `{"domain":"example.com"}`, 201)
	call("/v1/domains/example.com/batches", "mixed", batch(`{"email":"user@example.com"},{"email":"refused-address"}`), 201)
	call("/v1/domains/example.com/batches", "mixed", batch(`{"email":"user@example.com"},{"email":"refused-address"}`), 200)
	call("/v1/domains/example.com/batches", "all-refused", batch(`{"email":"invalid"},{"email":"also-invalid"}`), 422)

	want := `
# HELP kannon_batches_total Batches persisted at Intake, by the API they came in through. An idempotent replay is not counted.
# TYPE kannon_batches_total counter
kannon_batches_total{intake="rest"} 1
# HELP kannon_recipients_total Recipients decided at Intake: accepted into a Delivery, or rejected with none created.
# TYPE kannon_recipients_total counter
kannon_recipients_total{intake="rest",result="accepted"} 1
kannon_recipients_total{intake="rest",result="rejected"} 3
`
	assert.NoError(t, testutil.GatherAndCompare(reg, strings.NewReader(want), "kannon_batches_total", "kannon_recipients_total"))
}
