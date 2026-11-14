package health_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/kannon-email/kannon/pkg/api/health"
)

func TestHealth(t *testing.T) {
	failing := func(context.Context) error { return errors.New("postgres password secret") }
	for _, tc := range []struct {
		name   string
		ready  func(context.Context) error
		path   string
		status int
		body   string
	}{
		{"ready", func(context.Context) error { return nil }, "/health", 200, `{"status":"ready"}`},
		{"failing check", failing, "/health", 503, `{"status":"unavailable"}`},
		{"no check", nil, "/health", 503, `{"status":"unavailable"}`},
		{"other path", failing, "/v1/health", 404, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			health.NewHandler(tc.ready).ServeHTTP(w, httptest.NewRequest(http.MethodGet, tc.path, nil))
			if w.Code != tc.status {
				t.Fatalf("status %d, want %d", w.Code, tc.status)
			}
			if strings.Contains(w.Body.String(), "password") {
				t.Fatal("infrastructure detail leaked")
			}
			if tc.body != "" && strings.TrimSpace(w.Body.String()) != tc.body {
				t.Fatalf("body %s, want %s", w.Body, tc.body)
			}
		})
	}
}
