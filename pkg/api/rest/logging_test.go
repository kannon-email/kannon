package rest_test

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/kannon-email/kannon/internal/admintoken"
	"github.com/kannon-email/kannon/internal/apikeys"
	"github.com/kannon-email/kannon/internal/domains"
	"github.com/kannon-email/kannon/pkg/api/rest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// captureLogs routes the default logger to a buffer for the test and returns each line decoded.
func captureLogs(t *testing.T) func() []map[string]any {
	t.Helper()
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return func() []map[string]any {
		var lines []map[string]any
		for _, raw := range strings.Split(strings.TrimSpace(buf.String()), "\n") {
			if raw == "" {
				continue
			}
			var line map[string]any
			require.NoError(t, json.Unmarshal([]byte(raw), &line))
			lines = append(lines, line)
		}
		return lines
	}
}

// Each refused Bearer authentication is one log line naming the peer, the path and the request
// ID, and never what was presented.
func TestAFailedAuthenticationIsLoggedWithoutTheToken(t *testing.T) {
	const presented = "k_guessed-0123456789"
	for _, tc := range []struct {
		name, authorization string
	}{
		{"no credential", ""},
		{"not a bearer", "Basic " + presented},
		{"unknown credential", "Bearer " + presented},
	} {
		t.Run(tc.name, func(t *testing.T) {
			logs := captureLogs(t)
			keys := apikeys.NewService(failingKeyRepository{err: apikeys.ErrKeyNotFound})
			h := rest.NewHandler(rest.Services{Domains: domains.NewService(failingDomainRepository{}), Token: admintoken.MustParse(testAdminToken), Keys: keys})
			r := httptest.NewRequest(http.MethodGet, "/v1/domains", nil)
			r.RemoteAddr = "198.51.100.7:40000"
			if tc.authorization != "" {
				r.Header.Set("Authorization", tc.authorization)
			}
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)

			require.Equal(t, http.StatusUnauthorized, w.Code)
			lines := logs()
			require.Len(t, lines, 1)
			assert.Equal(t, "198.51.100.7:40000", lines[0]["peer"])
			assert.Equal(t, "/v1/domains", lines[0]["path"])
			assert.Equal(t, w.Header().Get("X-Request-ID"), lines[0]["request_id"])
			raw, err := json.Marshal(lines[0])
			require.NoError(t, err)
			assert.NotContains(t, string(raw), presented)
		})
	}
}

// A 500 tells the caller nothing, so the cause has to reach the log, under the request ID the
// caller was given, or nobody can find out what went wrong.
func TestAnInternalErrorIsLoggedUnderTheRequestIDTheCallerGets(t *testing.T) {
	logs := captureLogs(t)
	cause := &pgconn.PgError{Code: "22021", Message: "invalid byte sequence for encoding \"UTF8\": 0x00", Detail: "secret data"}
	h := rest.NewHandler(rest.Services{Domains: domains.NewService(failingDomainRepository{err: cause}), Token: admintoken.MustParse(testAdminToken)})
	r := httptest.NewRequest(http.MethodGet, "/v1/domains", nil)
	r.Header.Set("Authorization", "Bearer "+testAdminToken)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)

	require.Equal(t, http.StatusInternalServerError, w.Code)
	assert.NotContains(t, w.Body.String(), "UTF8")
	var p rest.Problem
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &p))
	var logged []map[string]any
	for _, line := range logs() {
		if line["request_id"] == p.RequestId {
			logged = append(logged, line)
		}
	}
	require.Len(t, logged, 1)
	assert.Equal(t, "ERROR", logged[0]["level"])
	assert.Contains(t, logged[0]["err"], "invalid byte sequence")
	assert.Equal(t, "/v1/domains", logged[0]["path"])
}
