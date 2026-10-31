package rest_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/kannon-email/kannon/internal/admintoken"
	"github.com/kannon-email/kannon/internal/apikeys"
	"github.com/kannon-email/kannon/internal/domains"
	"github.com/kannon-email/kannon/internal/intake"
	"github.com/kannon-email/kannon/internal/keyset"
	"github.com/kannon-email/kannon/internal/values"
	"github.com/kannon-email/kannon/pkg/api/rest"
)

// Exercise the mounted HTTP adapter through the existing repository seam.
type failingDomainRepository struct {
	domains.Repository
	err error
}

func (r failingDomainRepository) ListPage(context.Context, keyset.Page[values.DomainName]) ([]*domains.Domain, error) {
	return nil, r.err
}

type failingKeyRepository struct {
	apikeys.Repository
	err error
}

func (r failingKeyRepository) FindByKeyHash(context.Context, string) (*apikeys.APIKey, error) {
	return nil, r.err
}

// A storage outage while resolving a Bearer credential is not a bad credential.
func TestCredentialLookupFailure(t *testing.T) {
	for _, tc := range []struct {
		name   string
		err    error
		status int
	}{
		{"unknown credential", apikeys.ErrKeyNotFound, 401},
		{"storage outage", fmt.Errorf("private: %w", context.DeadlineExceeded), 500},
	} {
		t.Run(tc.name, func(t *testing.T) {
			keys := apikeys.NewService(failingKeyRepository{err: tc.err})
			h := rest.NewHandler(domains.NewService(failingDomainRepository{}), admintoken.MustParse("secret"), nil, keys)
			r := httptest.NewRequest(http.MethodGet, "/v1beta1/domains", nil)
			r.Header.Set("Authorization", "Bearer k_unknown")
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			if w.Code != tc.status {
				t.Fatalf("unexpected response: %d %s", w.Code, w.Body)
			}
		})
	}
}

func TestApplicationErrorResponses(t *testing.T) {
	for _, tc := range []struct {
		name        string
		err         error
		status      int
		code, retry string
	}{
		{"unrelated uniqueness", &pgconn.PgError{Code: "23505", ConstraintName: "private_constraint", Detail: "secret data"}, 500, "internal_error", ""},
		{"domain conflict", fmt.Errorf("private context: %w", domains.ErrDomainAlreadyExists), 409, "domain_already_exists", ""},
		{"retryable receipt", fmt.Errorf("private context: %w", intake.ErrIdempotencyInProgress), 409, "idempotency_in_progress", "1"},
		{"permanent receipt", intake.ErrIdempotencyConflict, 409, "idempotency_conflict", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := rest.NewHandler(domains.NewService(failingDomainRepository{err: tc.err}), admintoken.MustParse("secret"), nil)
			r := httptest.NewRequest(http.MethodGet, "/v1beta1/domains", nil)
			r.Header.Set("Authorization", "Bearer secret")
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			var p rest.Problem
			if err := json.Unmarshal(w.Body.Bytes(), &p); err != nil {
				t.Fatal(err)
			}
			if w.Code != tc.status || p.Status != tc.status || p.Code != tc.code || p.Type != "urn:kannon:problem:"+tc.code {
				t.Fatalf("unexpected response: %d %s", w.Code, w.Body)
			}
			if p.RequestId == "" || p.RequestId != w.Header().Get("X-Request-ID") {
				t.Fatal("missing or mismatched request ID")
			}
			if w.Header().Get("Content-Type") != "application/problem+json" || w.Header().Get("Retry-After") != tc.retry || w.Header().Get("WWW-Authenticate") != "" {
				t.Fatalf("unexpected headers: %v", w.Header())
			}
			if strings.Contains(w.Body.String(), "private") || strings.Contains(w.Body.String(), "secret") {
				t.Fatal("internal details leaked")
			}
		})
	}
}
