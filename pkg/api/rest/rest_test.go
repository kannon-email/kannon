package rest_test

import (
	"context"
	"encoding/json"
	"errors"
	schema "github.com/kannon-email/kannon/db"
	"github.com/kannon-email/kannon/internal/admintoken"
	sqlc "github.com/kannon-email/kannon/internal/db"
	"github.com/kannon-email/kannon/internal/domains"
	"github.com/kannon-email/kannon/internal/tests"
	"github.com/kannon-email/kannon/pkg/api/rest"
	"go.yaml.in/yaml/v3"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func TestMain(m *testing.M) {
	tests.Main(m.Run)
}

func TestPublicReadiness(t *testing.T) {
	for _, ready := range []bool{true, false} {
		h := rest.NewHandler(rest.Services{Token: admintoken.MustParse("secret"), Ready: func(context.Context) error {
			if !ready {
				return errors.New("postgres password secret")
			}
			return nil
		}})
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/v1beta1/health", nil))
		want := 200
		if !ready {
			want = 503
		}
		if w.Code != want {
			t.Fatalf("status %d: %s", w.Code, w.Body)
		}
		if strings.Contains(w.Body.String(), "password") {
			t.Fatal("infrastructure leak")
		}
		if w.Header().Get("X-Request-ID") == "" {
			t.Fatal("missing request id")
		}
	}
}

func TestAuthenticationAndInvalidRequests(t *testing.T) {
	h := rest.NewHandler(rest.Services{Token: admintoken.MustParse("secret")})
	for _, tc := range []struct {
		method, path, body, token, code string
		status                          int
	}{
		{"GET", "/v1beta1/domains", "", "", "unauthenticated", 401},
		{"GET", "/v1beta1/domains?wat=1", "", "secret", "unknown_query_parameter", 400},
		{"GET", "/v1beta1/domains?limit=1001", "", "secret", "invalid_query_parameter", 400},
		{"GET", "/v1beta1/domains?cursor=garbage", "", "secret", "invalid_cursor", 400},
		{"POST", "/v1beta1/domains", `{"domain":"example.com","name":"rename"}`, "secret", "malformed_request", 400},
		{"PATCH", "/v1beta1/domains/example.com", `{"tracking":{"opens":"full"}}`, "secret", "validation_failed", 422},
	} {
		t.Run(tc.code+tc.method, func(t *testing.T) {
			r := httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
			if tc.token != "" {
				r.Header.Set("Authorization", "Bearer "+tc.token)
			}
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			if w.Code != tc.status || !strings.Contains(w.Body.String(), `"code":"`+tc.code+`"`) {
				t.Fatalf("%d %s", w.Code, w.Body)
			}
			if tc.status == 401 && w.Header().Get("WWW-Authenticate") != "Bearer" {
				t.Fatal("missing Bearer challenge")
			}
		})
	}
}

func TestPersistentDomainJourney(t *testing.T) {
	if testing.Short() {
		t.Skip("requires Docker PostgreSQL")
	}
	db, purge, err := tests.TestPostgresInit(schema.Schema)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		db.Close()
		if err := purge(); err != nil {
			t.Error(err)
		}
	}()
	h := rest.NewHandler(rest.Services{Domains: domains.NewService(sqlc.NewDomainsRepository(db)), Token: admintoken.MustParse("secret"), Ready: db.Ping})
	call := func(method, path, body string, status int) string {
		t.Helper()
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		r.Header.Set("Authorization", "Bearer secret")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != status {
			t.Fatalf("%s %s: %d %s", method, path, w.Code, w.Body)
		}
		if status == 201 && w.Header().Get("Location") == "" {
			t.Fatal("missing Location")
		}
		return w.Body.String()
	}
	for _, name := range []string{"b.example.com", "a.example.com"} {
		call("POST", "/v1beta1/domains", `{"domain":"`+name+`"}`, 201)
	}
	duplicate := call("POST", "/v1beta1/domains", `{"domain":"a.example.com"}`, 409)
	if !strings.Contains(duplicate, `"code":"domain_already_exists"`) {
		t.Fatalf("unexpected duplicate error: %s", duplicate)
	}
	var page struct {
		Items      []struct{ Domain string }
		NextCursor string `json:"next_cursor"`
	}
	if err := json.Unmarshal([]byte(call("GET", "/v1beta1/domains?limit=1", "", 200)), &page); err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 1 || page.Items[0].Domain != "a.example.com" || page.NextCursor == "" {
		t.Fatalf("unexpected page: %+v", page)
	}
	next := call("GET", "/v1beta1/domains?limit=1&cursor="+page.NextCursor, "", 200)
	if !strings.Contains(next, "b.example.com") || strings.Contains(next, "a.example.com") {
		t.Fatal(next)
	}
	call("PATCH", "/v1beta1/domains/a.example.com", `{"tracking":{"opens":"anonymous","links":"off"}}`, 200)
	body := call("GET", "/v1beta1/domains/a.example.com", "", 200)
	if !strings.Contains(body, `"opens":"anonymous"`) || !strings.Contains(body, "dkim_public_key") || strings.Contains(body, "private") {
		t.Fatal(body)
	}
	call("GET", "/v1beta1/domains/missing.example.com", "", 404)
}

func TestApprovedContract(t *testing.T) {
	b, err := os.ReadFile("openapi.yaml")
	if err != nil {
		t.Fatal(err)
	}
	var contract struct {
		OpenAPI    string                    `yaml:"openapi"`
		Paths      map[string]map[string]any `yaml:"paths"`
		Components struct {
			Schemas map[string]any `yaml:"schemas"`
		} `yaml:"components"`
	}
	if err := yaml.Unmarshal(b, &contract); err != nil {
		t.Fatal(err)
	}
	operations := 0
	for _, path := range contract.Paths {
		for method := range path {
			switch method {
			case "get", "post", "patch", "delete":
				operations++
			}
		}
	}
	if contract.OpenAPI != "3.1.1" || operations != 21 || len(contract.Components.Schemas) != 48 {
		t.Fatalf("approved contract changed: version=%s operations=%d schemas=%d", contract.OpenAPI, operations, len(contract.Components.Schemas))
	}
}
