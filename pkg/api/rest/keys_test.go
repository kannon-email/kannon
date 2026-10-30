package rest_test

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/jackc/pgx/v5/pgxpool"
	schema "github.com/kannon-email/kannon/db"
	"github.com/kannon-email/kannon/internal/admintoken"
	"github.com/kannon-email/kannon/internal/apikeys"

	sqlc "github.com/kannon-email/kannon/internal/db"
	"github.com/kannon-email/kannon/internal/domains"
	"github.com/kannon-email/kannon/internal/tests"
	"github.com/kannon-email/kannon/internal/values"
	"github.com/kannon-email/kannon/pkg/api/rest"
	"net/http/httptest"

	"strings"
	"testing"
	"time"
)

func TestPersistentCredentialJourney(t *testing.T) {
	if testing.Short() {
		t.Skip("requires PostgreSQL via Docker")
	}
	db, purge, err := credentialDB()
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		db.Close()
		if err := purge(); err != nil {
			t.Error(err)
		}
	}()
	keys := apikeys.NewService(sqlc.NewAPIKeysRepository(db))
	h := rest.NewHandler(domains.NewService(sqlc.NewDomainsRepository(db)), admintoken.MustParse("secret"), db.Ping, keys)
	call := func(token, method, path, body string, status int) string {
		t.Helper()
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		r.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != status {
			t.Fatalf("%s %s: %d %s", method, path, w.Code, w.Body)
		}
		return w.Body.String()
	}
	call("secret", "POST", "/v1beta1/domains", `{"domain":"example.com"}`, 201)
	base := "/v1beta1/domains/example.com/api-keys"
	var key rest.APIKeyCreated
	if err := json.Unmarshal([]byte(call("secret", "POST", base, `{"name":"operator","roles":["admin"]}`, 201)), &key); err != nil {
		t.Fatal(err)
	}
	if key.Secret == "" {
		t.Fatal("missing secret")
	}
	call(key.Secret, "GET", "/v1beta1/domains/example.com", "", 200)
	call(key.Secret, "GET", "/v1beta1/domains", "", 403)
	call(key.Secret, "GET", "/v1beta1/domains/other.com", "", 403)
	call(key.Secret, "POST", "/v1beta1/domains/other.com/api-keys", `{"name":"outside","roles":["admin"]}`, 403)
	body := call(key.Secret, "GET", base+"/"+key.Id, "", 200)
	if strings.Contains(body, key.Secret) || strings.Contains(body, "secret") {
		t.Fatal("secret in metadata")
	}
	call(key.Secret, "POST", base, `{"name":"bad","roles":["owner"]}`, 422)
	call(key.Secret, "POST", base, `{"name":"bad","roles":[]}`, 422)
	call(key.Secret, "POST", base, `{"name":"bad","roles":["sender"],"expires_at":"2000-01-01T00:00:00Z"}`, 422)
	call(key.Secret, "PATCH", base+"/"+key.Id, `{"roles":["sender"]}`, 400)
	call(key.Secret, "POST", base, `{"name":"bad","roles":["sender"],"expires_at":null}`, 422)
	// Legacy issuance has no role metadata, and authenticates without rewriting the row.
	domain := values.MustParse("example.com")

	legacy, err := apikeys.NewAPIKey(domain, "legacy", nil)
	if err != nil {
		t.Fatal(err)
	}
	// Seed exactly the pre-upgrade shape, omitting roles.
	_, err = db.Exec(context.Background(), "INSERT INTO api_keys(id,domain,name,key_hash,key_prefix) VALUES($1,$2,$3,$4,$5)", legacy.Key.ID().String(), domain.String(), legacy.Key.Name(), legacy.Key.KeyHash(), legacy.Key.KeyPrefix())
	if err != nil {
		t.Fatal(err)
	}

	call(legacy.PlaintextKey, "GET", "/v1beta1/domains/example.com", "", 403)
	resolved, err := keys.ValidateForAuth(context.Background(), domain, legacy.PlaintextKey)
	if err != nil || resolved.ID() != legacy.Key.ID() || resolved.KeyHash() != legacy.Key.KeyHash() {
		t.Fatal("legacy credential changed", err)
	}
	var storedRoles []string
	if err := db.QueryRow(context.Background(), "SELECT roles FROM api_keys WHERE id=$1", legacy.Key.ID().String()).Scan(&storedRoles); err != nil || storedRoles != nil {
		t.Fatal("legacy row was rewritten", err)
	}

	for _, role := range []string{"sender", "batch-reader", "analyst", "metrics-reader"} {
		var issued rest.APIKeyCreated
		if err := json.Unmarshal([]byte(call(key.Secret, "POST", base, `{"name":"reader","roles":["`+role+`"]}`, 201)), &issued); err != nil {
			t.Fatal(err)
		}
		call(issued.Secret, "GET", base, "", 403)
		call(issued.Secret, "POST", base, `{"name":"escalation","roles":["admin"]}`, 403)
	}
	var composed rest.APIKeyCreated
	if err := json.Unmarshal([]byte(call(key.Secret, "POST", base, `{"name":"composed","roles":["sender","batch-reader","analyst","metrics-reader"],"expires_at":"2099-01-01T00:00:00Z"}`, 201)), &composed); err != nil {
		t.Fatal(err)
	}
	principal, err := keys.Authenticate(context.Background(), composed.Secret)
	if err != nil || principal.ID() != composed.Id+"@example.com" || len(principal.Grants()) != 4 {
		t.Fatal("composed credential identity or grants", err)
	}
	// Equal timestamps exercise the descending ID tie-breaker.
	if _, err := db.Exec(context.Background(), "UPDATE api_keys SET created_at='2026-01-01' WHERE domain='example.com'"); err != nil {
		t.Fatal(err)
	}

	var page rest.APIKeyPage
	if err := json.Unmarshal([]byte(call(key.Secret, "GET", base+"?limit=1", "", 200)), &page); err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 1 || page.NextCursor == nil {
		t.Fatal("missing pagination")
	}
	next := call(key.Secret, "GET", base+"?limit=1&cursor="+*page.NextCursor, "", 200)
	var second rest.APIKeyPage
	if err := json.Unmarshal([]byte(next), &second); err != nil {
		t.Fatal(err)
	}
	if len(second.Items) != 1 || second.Items[0].Id >= page.Items[0].Id {
		t.Fatal("incorrect ID tie-breaker")
	}
	if strings.Contains(next, page.Items[0].Id) {
		t.Fatal("repeated page item")
	}
	call(key.Secret, "PATCH", "/v1beta1/domains/example.com", `{"tracking":{"opens":"off","links":"off"}}`, 200)
	// Attribution is validated, stays attached to the authenticated identity, and is permission checked.
	for _, tc := range []struct {
		token, claim string
		status       int
	}{
		{key.Secret, "operator", 200}, {key.Secret, "   ", 400}, {legacy.PlaintextKey, "operator", 403},
	} {
		r := httptest.NewRequest(http.MethodGet, base, nil)
		r.Header.Set("Authorization", "Bearer "+tc.token)
		r.Header.Set("X-Kannon-Attribution", tc.claim)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != tc.status {
			t.Fatalf("attribution: %d %s", w.Code, w.Body)
		}
	}
	// Expired stored credentials cannot authenticate in either transport.
	expired := apikeys.LoadAPIKey(apikeys.LoadAPIKeyParams{ID: apikeys.NewID(), Domain: domain, Name: "expired", KeyHash: apikeys.HashKey("expired-secret"), KeyPrefix: "expired", IsActive: true, ExpiresAt: timePtr(time.Now().Add(-time.Hour))})
	if err := sqlc.NewAPIKeysRepository(db).Create(context.Background(), expired); err != nil {
		t.Fatal(err)
	}
	call("expired-secret", "GET", base, "", 401)
	if _, err := keys.ValidateForAuth(context.Background(), domain, "expired-secret"); err == nil {
		t.Fatal("expired gRPC credential accepted")
	}

	call("secret", "PATCH", base+"/"+key.Id, `{"status":"revoked"}`, 200)
	call("secret", "PATCH", base+"/"+key.Id, `{"status":"revoked"}`, 200)
	call(key.Secret, "GET", base, "", 401)
}

// credentialDB accepts a disposable local PostgreSQL database as well as Docker.
func credentialDB() (*pgxpool.Pool, func() error, error) {
	return tests.TestPostgresInit(schema.Schema)
}

func timePtr(t time.Time) *time.Time { return &t }
