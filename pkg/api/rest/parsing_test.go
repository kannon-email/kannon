package rest_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/kannon-email/kannon/internal/admintoken"
	"github.com/kannon-email/kannon/internal/apikeys"
	sqlc "github.com/kannon-email/kannon/internal/db"
	"github.com/kannon-email/kannon/internal/domains"
	"github.com/kannon-email/kannon/internal/intake"
	"github.com/kannon-email/kannon/internal/stats"
	"github.com/kannon-email/kannon/internal/templates"
	"github.com/kannon-email/kannon/pkg/api/rest"
	"github.com/stretchr/testify/require"
)

func TestParsingHTTPContract(t *testing.T) {
	db, cleanup, err := credentialDB()
	require.NoError(t, err)
	defer func() { db.Close(); require.NoError(t, cleanup()) }()
	h := rest.NewHandler(rest.Services{
		Domains:   domains.NewService(sqlc.NewDomainsRepository(db)),
		Token:     admintoken.MustParse("secret"),
		Ready:     db.Ping,
		Inline:    intake.NewInlineService(sqlc.NewInlineStore(db), sqlc.NewDomainsRepository(db), nil, 0, 0),
		Stats:     stats.NewService(stats.NewInMemRepository(), stats.WithAggregatedStatsRepository(stats.NewInMemAggregatedStatsRepository())),
		Templates: templates.NewService(sqlc.NewTemplatesRepository(db)),
		Keys:      apikeys.NewService(sqlc.NewAPIKeysRepository(db)),
	})
	call := func(method, path, body, key string, status int, code string) map[string]any {
		t.Helper()
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		r.Header.Set("Authorization", "Bearer secret")
		if key != "" {
			r.Header.Set("Idempotency-Key", key)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		require.Equal(t, status, w.Code, "%s %s: %s", method, path, w.Body)
		var out map[string]any
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &out))
		if code != "" {
			require.Equal(t, code, out["code"])
			require.Equal(t, w.Header().Get("X-Request-ID"), out["request_id"])
			require.NotEmpty(t, out["request_id"])
			require.Equal(t, "application/problem+json", w.Header().Get("Content-Type"))
		}
		return out
	}
	const domain = "/v1beta1/domains/example.com"
	call("POST", "/v1beta1/domains", `{"domain":"example.com"}`, "", 201, "")
	batchBody := `{"from":{"email":"sender@example.com"},"subject":"Hello","content":{"type":"html","html":"Hello"},"recipients":[{"email":"ok@example.com"},{}]}`
	created := call("POST", domain+"/batches", batchBody, "initial", 201, "")
	require.Equal(t, float64(1), created["accepted_count"])
	require.Equal(t, float64(1), created["rejected_count"])
	batchPath := domain + "/batches/" + fmt.Sprint(created["id"])
	key := call("POST", domain+"/api-keys", `{"name":"reader","roles":["sender"]}`, "", 201, "")
	keyPath := domain + "/api-keys/" + fmt.Sprint(key["id"])
	template := call("POST", domain+"/templates", `{"title":"","html":"Hello"} `, "", 201, "")
	templatePath := domain + "/templates/" + fmt.Sprint(template["id"])
	const span = "from=2026-01-01T00:00:00Z&to=2026-01-01T01:00:00Z"
	collections := []string{"/v1beta1/domains", domain + "/templates", domain + "/api-keys", domain + "/batches", batchPath + "/deliveries", domain + "/stats/events"}
	for _, path := range collections {
		t.Run("query "+path, func(t *testing.T) {
			prefix := path + "?"
			if strings.HasSuffix(path, "/events") {
				prefix += span + "&"
			}
			for _, tc := range []struct{ query, code string }{
				{"bad=%zz", "invalid_query_parameter"}, {"limit=1;cursor=x", "invalid_query_parameter"},
				{"unknown=1", "unknown_query_parameter"}, {"unknown=1&unknown=2", "unknown_query_parameter"},
				{"limit=1&limit=2", "invalid_query_parameter"}, {"cursor=x&cursor=y", "invalid_query_parameter"},
				{"limit=0", "invalid_query_parameter"}, {"limit=1001", "invalid_query_parameter"}, {"limit=", "invalid_query_parameter"}, {"limit=no", "invalid_query_parameter"},
				{"cursor=", "invalid_cursor"}, {"cursor=bad", "invalid_cursor"},
			} {
				call("GET", prefix+tc.query, "", "", 400, tc.code)
			}
			for _, limit := range []string{"", "1", "100", "1000", "%2B1"} {
				query := ""
				if limit != "" {
					query = "limit=" + limit
				}
				call("GET", prefix+query, "", "", 200, "")
			}
		})
	}
	for _, path := range []string{domain, templatePath, keyPath, batchPath, batchPath + "/deliveries/opaque"} {
		for _, query := range []string{"x=1", "x=1&x=2", "limit=1"} {
			call("GET", path+"?"+query, "", "", 400, "unknown_query_parameter")
		}
		call("GET", path+"?x=%zz", "", "", 400, "invalid_query_parameter")
	}
	for _, query := range []string{"status=", "status=unknown", "status=pending&status=scheduled"} {
		call("GET", batchPath+"/deliveries?"+query, "", "", 400, "invalid_query_parameter")
	}
	for _, query := range []string{"batch_id=", "batch_id=x&batch_id=y", "from=x&from=y", "to=x&to=y"} {
		call("GET", domain+"/stats/events?"+span+"&"+query, "", "", 400, "invalid_query_parameter")
	}
	call("GET", domain+"/stats/events?"+span+"&type=validated&type=validated&type=delivered", "", "", 200, "")
	for _, query := range []string{"from=x", "to=x", "group_by=2h"} {
		call("GET", domain+"/stats/aggregated?"+span+"&group_by=1h&"+query, "", "", 400, "invalid_query_parameter")
	}
	call("GET", domain+"/stats/aggregated?"+span+"&group_by=1h&batch_id=x", "", "", 400, "unknown_query_parameter")

	for _, route := range []struct{ method, path string }{
		{"POST", "/v1beta1/domains"}, {"PATCH", domain}, {"POST", domain + "/templates"}, {"PATCH", templatePath},
		{"POST", domain + "/api-keys"}, {"PATCH", keyPath}, {"POST", domain + "/batches"},
	} {
		t.Run("body "+route.method+route.path, func(t *testing.T) {
			for _, body := range []string{"", "{", "[]", `{"unknown":true}`, "{} {}", "{} garbage"} {
				call(route.method, route.path, body, "invalid", 400, "malformed_request")
			}
			call(route.method, route.path, "null", "invalid", 422, "validation_failed")
			// The complete body, including trailing whitespace, counts against the limit.
			exact := "{}" + strings.Repeat(" ", (10<<20)-2)
			call(route.method, route.path, exact, "invalid", 422, "validation_failed")
			call(route.method, route.path, exact+" ", "invalid", 413, "request_body_too_large")
		})
	}
	call("POST", domain+"/templates", `{"html":"Hello"}`, "", 422, "validation_failed")
	call("PATCH", templatePath, `{"title":null}`, "", 422, "validation_failed")
	for _, extra := range []string{`,"scheduled_at":null`, `,"headers":null`, `,"tracking":{"opens":null}`, `,"global_fields":{"a~/b":null}`} {
		out := call("POST", domain+"/batches", strings.TrimSuffix(batchBody, "}")+extra+"}", "invalid", 422, "validation_failed")
		if strings.Contains(extra, "a~/b") {
			require.Contains(t, fmt.Sprint(out["errors"]), "/global_fields/a~0~1b")
		}
	}
	for _, content := range []struct {
		value  string
		status int
	}{
		{`{}`, 422}, {`{"type":"html"}`, 422}, {`{"type":"html","html":""}`, 422},
		{`{"type":"html","html":"Hello","template_id":"x"}`, 422},
		{`{"type":"html","html":"Hello","unknown":1}`, 400},
	} {
		code := "validation_failed"
		if content.status == 400 {
			code = "malformed_request"
		}
		call("POST", domain+"/batches", strings.Replace(batchBody, `{"type":"html","html":"Hello"}`, content.value, 1), "invalid", content.status, code)
	}
	for _, tc := range []struct {
		attachment string
		status     int
		code       string
	}{
		{`{"filename":"a.txt"}`, 422, "validation_failed"},
		{`{"filename":"a.txt","content_base64":[65]}`, 400, "malformed_request"},
		{`{"filename":"a.txt","content_base64":"!"}`, 400, "malformed_request"},
		{`{"filename":"a.txt","content_base64":""}`, 201, ""},
	} {
		body := strings.TrimSuffix(batchBody, "}") + `,"attachments":[` + tc.attachment + `]}`
		call("POST", domain+"/batches", body, "attachment", tc.status, tc.code)
		if tc.status == 201 {
			call("POST", domain+"/batches", body, "attachment", 200, "")
		}
	}
	// Missing recipient email remains a row refusal, and generated DTO conversion
	// must preserve empty policy/attachment shapes and nil versus empty maps.
	replayBody := strings.TrimSuffix(batchBody, "}") + `,"tracking":{},"attachments":[]}`
	replay := call("POST", domain+"/batches", replayBody, "initial", 200, "")
	require.Equal(t, created["id"], replay["id"])
	require.NotContains(t, replay, "rejected_recipients")
	emptyMapBody := strings.TrimSuffix(batchBody, "}") + `,"global_fields":{}}`
	call("POST", domain+"/batches", emptyMapBody, "initial", 409, "idempotency_conflict")
	call("POST", domain+"/batches", emptyMapBody, "empty-map", 201, "")
	call("POST", domain+"/batches", emptyMapBody, "empty-map", 200, "")
	deliveries := call("GET", batchPath+"/deliveries", "", "", 200, "")
	require.Len(t, deliveries["items"], 1)

	// History cursors retain their scope while page size can change.
	page := call("GET", domain+"/batches?limit=1", "", "", 200, "")
	require.NotNil(t, page["next_cursor"])
	cursor := fmt.Sprint(page["next_cursor"])
	call("GET", domain+"/batches?limit=1000&cursor="+cursor, "", "", 200, "")
	call("GET", "/v1beta1/domains/other.com/batches?cursor="+cursor, "", "", 400, "invalid_cursor")
	call("GET", batchPath+"/deliveries?cursor="+cursor, "", "", 400, "invalid_cursor")
	manyBody := strings.Replace(batchBody, `[{"email":"ok@example.com"},{}]`, `[{"email":"a@example.com"},{"email":"b@example.com"},{"email":"c@example.com"}]`, 1)
	many := call("POST", domain+"/batches", manyBody, "many", 201, "")
	manyPath := domain + "/batches/" + fmt.Sprint(many["id"]) + "/deliveries"
	page = call("GET", manyPath+"?limit=1", "", "", 200, "")
	require.NotNil(t, page["next_cursor"])
	cursor = fmt.Sprint(page["next_cursor"])
	next := call("GET", manyPath+"?limit=1000&cursor="+cursor, "", "", 200, "")
	require.Len(t, next["items"], 2)
	call("GET", manyPath+"?status=pending&cursor="+cursor, "", "", 400, "invalid_cursor")
	call("GET", batchPath+"/deliveries?cursor="+cursor, "", "", 400, "invalid_cursor")
	// Page size is adapter syntax for every list, so it is refused before authorization.
	outOfRange := httptest.NewRequest(http.MethodGet, domain+"/stats/events?"+span+"&limit=0", nil)
	outOfRange.Header.Set("Authorization", "Bearer "+fmt.Sprint(key["secret"]))
	outOfRangeResponse := httptest.NewRecorder()
	h.ServeHTTP(outOfRangeResponse, outOfRange)
	require.Equal(t, 400, outOfRangeResponse.Code)
	// Authentication still precedes malformed body parsing.
	r := httptest.NewRequest(http.MethodPost, domain+"/batches", strings.NewReader("{"))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	require.Equal(t, 401, w.Code)
}

type brokenBody struct{}

func (brokenBody) Read([]byte) (int, error) { return 0, errors.New("private reader error") }
func (brokenBody) Close() error             { return nil }

func TestBodyReadFailureHTTP(t *testing.T) {
	h := rest.NewHandler(rest.Services{Token: admintoken.MustParse("secret")})
	r := httptest.NewRequest(http.MethodPost, "/v1beta1/domains", nil)
	r.Body = io.ReadCloser(brokenBody{})
	r.Header.Set("Authorization", "Bearer secret")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	require.Equal(t, 400, w.Code)
	require.Contains(t, w.Body.String(), `"code":"malformed_request"`)
	require.NotContains(t, w.Body.String(), "private")
}

// Exercise net/http's own oversized-body response/draining behavior as well as
// the recorder tests. A subsequent request must work; connection reuse is not
// part of the API contract.
func TestOversizedBodyHTTPServer(t *testing.T) {
	h := rest.NewHandler(rest.Services{Token: admintoken.MustParse("secret")})
	server := httptest.NewServer(h)
	defer server.Close()
	request, err := http.NewRequest(http.MethodPost, server.URL+"/v1beta1/domains", strings.NewReader(strings.Repeat(" ", (10<<20)+1)))
	require.NoError(t, err)
	request.Header.Set("Authorization", "Bearer secret")
	response, err := server.Client().Do(request)
	require.NoError(t, err)
	body, err := io.ReadAll(response.Body)
	require.NoError(t, err)
	require.NoError(t, response.Body.Close())
	require.Equal(t, 413, response.StatusCode)
	require.Contains(t, string(body), `"code":"request_body_too_large"`)
	response, err = server.Client().Get(server.URL + "/v1beta1/health")
	require.NoError(t, err)
	require.NoError(t, response.Body.Close())
	require.Equal(t, 503, response.StatusCode)
}
