package rest_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"connectrpc.com/connect"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/kannon-email/kannon/internal/admintoken"
	"github.com/kannon-email/kannon/internal/apikeys"
	"github.com/kannon-email/kannon/internal/apperr"
	"github.com/kannon-email/kannon/internal/authz"
	"github.com/kannon-email/kannon/internal/domains"
	"github.com/kannon-email/kannon/internal/intake"
	"github.com/kannon-email/kannon/internal/stats"
	"github.com/kannon-email/kannon/internal/templates"
	"github.com/kannon-email/kannon/pkg/api/connecterr"
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
			h := rest.NewHandler(rest.Services{Domains: domains.NewService(failingDomainRepository{}), Token: admintoken.MustParse("secret"), Keys: keys})
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

// Each application Kind maps once per transport; both mappings are shown side by side so a
// sentinel cannot reach REST and gRPC as different kinds of refusal.
func TestApplicationErrorResponses(t *testing.T) {
	for _, tc := range []struct {
		name        string
		err         error
		status      int
		code, retry string
		grpc        connect.Code
	}{
		{"unrelated uniqueness", &pgconn.PgError{Code: "23505", ConstraintName: "private_constraint", Detail: "secret data"}, 500, "internal_error", "", connect.CodeInternal},
		{"forbidden", fmt.Errorf("private context: %w", authz.ErrForbidden), 403, "forbidden", "", connect.CodePermissionDenied},
		{"no principal", authz.ErrNoPrincipal, 403, "forbidden", "", connect.CodePermissionDenied},
		{"domain conflict", fmt.Errorf("private context: %w", domains.ErrDomainAlreadyExists), 409, "domain_already_exists", "", connect.CodeAlreadyExists},
		{"retryable receipt", fmt.Errorf("private context: %w", intake.ErrIdempotencyInProgress), 409, "idempotency_in_progress", "1", connect.CodeAlreadyExists},
		{"permanent receipt", intake.ErrIdempotencyConflict, 409, "idempotency_conflict", "", connect.CodeAlreadyExists},
		{"template not found", templates.ErrTemplateNotFound, 404, "not_found", "", connect.CodeNotFound},
		{"key not found", apikeys.ErrKeyNotFound, 404, "not_found", "", connect.CodeNotFound},
		{"domain not found", domains.ErrDomainNotFound, 404, "not_found", "", connect.CodeNotFound},
		{"delivery not found", intake.ErrDeliveryNotFound, 404, "not_found", "", connect.CodeNotFound},
		{"batch not found", intake.ErrBatchNotFound, 404, "not_found", "", connect.CodeNotFound},
		{"invalid roles", apikeys.ErrInvalidRoles, 422, "validation_failed", "", connect.CodeInvalidArgument},
		{"invalid key name", apikeys.ErrInvalidName, 422, "validation_failed", "", connect.CodeInvalidArgument},
		{"expiry", apikeys.ErrExpiryNotFuture, 422, "expiry_not_future", "", connect.CodeInvalidArgument},
		{"schedule", intake.ErrScheduledAtNotFuture, 422, "scheduled_at_not_future", "", connect.CodeInvalidArgument},
		{"attachment names", intake.ErrDuplicateAttachmentFilename, 422, "duplicate_attachment_filename", "", connect.CodeInvalidArgument},
		{"recipient limit", intake.ErrTooManyRecipients, 422, "too_many_recipients", "", connect.CodeInvalidArgument},
		{"wrapped validation cause", apperr.Wrap(apperr.Invalid, "validation_failed", errors.New("private cause")), 422, "validation_failed", "", connect.CodeInvalidArgument},
		{"event query", stats.ErrEventQuery, 400, "invalid_query_parameter", "", connect.CodeInvalidArgument},
		{"aggregate query", stats.ErrAggregateQuery, 400, "invalid_query_parameter", "", connect.CodeInvalidArgument},
	} {
		t.Run(tc.name+"/grpc", func(t *testing.T) {
			got := connecterr.Error(tc.err)
			if got.Code() != tc.grpc || !errors.Is(got, tc.err) {
				t.Fatalf("unexpected Connect error: %v", got)
			}
		})
		t.Run(tc.name, func(t *testing.T) {
			h := rest.NewHandler(rest.Services{Domains: domains.NewService(failingDomainRepository{err: tc.err}), Token: admintoken.MustParse("secret")})
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
