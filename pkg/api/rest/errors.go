package rest

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/kannon-email/kannon/internal/apikeys"
	"github.com/kannon-email/kannon/internal/authz"
	"github.com/kannon-email/kannon/internal/domains"
	"github.com/kannon-email/kannon/internal/intake"
	"github.com/kannon-email/kannon/internal/stats"
	"github.com/kannon-email/kannon/internal/templates"
)

// handler owns error responses. Operations write success only and must return
// before writing any response when they fail.
type handler func(http.ResponseWriter, *http.Request) error

func (h handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if err := h(w, r); err != nil {
		writeError(w, err)
	}
}

// requestError contains only adapter-authored, public details. Never populate it
// with infrastructure error text. Application errors are mapped below.
type requestError struct {
	status int
	code   string
	fields []FieldError
}

func (e *requestError) Error() string { return e.code }

func problemError(status int, code string) *requestError {
	return &requestError{status: status, code: code}
}

func errorProblem(err error) *requestError {
	var request *requestError
	var invalid *intake.InvalidRequest
	switch {
	case errors.As(err, &request):
		return request
	case errors.Is(err, authz.ErrForbidden), errors.Is(err, authz.ErrNoPrincipal):
		return problemError(403, "forbidden")
	case errors.Is(err, apikeys.ErrInvalidRoles), errors.Is(err, apikeys.ErrInvalidName):
		return problemError(422, "validation_failed")
	case errors.Is(err, apikeys.ErrExpiryNotFuture):
		return problemError(422, "expiry_not_future")
	case errors.Is(err, templates.ErrTemplateNotFound), errors.Is(err, apikeys.ErrKeyNotFound), errors.Is(err, domains.ErrDomainNotFound), errors.Is(err, intake.ErrDeliveryNotFound):
		return problemError(404, "not_found")
	case errors.Is(err, domains.ErrDomainAlreadyExists):
		return problemError(409, "domain_already_exists")
	case errors.Is(err, intake.ErrIdempotencyConflict):
		return problemError(409, "idempotency_conflict")
	case errors.Is(err, intake.ErrIdempotencyInProgress):
		return problemError(409, "idempotency_in_progress")
	case errors.Is(err, intake.ErrScheduledAtNotFuture):
		return problemError(422, "scheduled_at_not_future")
	case errors.Is(err, intake.ErrDuplicateAttachmentFilename):
		return problemError(422, "duplicate_attachment_filename")
	case errors.Is(err, intake.ErrTooManyRecipients):
		return problemError(422, "too_many_recipients")
	case errors.Is(err, stats.ErrEventCursor):
		return problemError(400, "invalid_cursor")
	case errors.Is(err, stats.ErrEventQuery), errors.Is(err, stats.ErrAggregateQuery):
		return problemError(400, "invalid_query_parameter")
	case errors.As(err, &invalid):
		return problemError(422, "validation_failed")
	default:
		return problemError(500, "internal_error")
	}
}

func writeError(w http.ResponseWriter, err error) {
	e := errorProblem(err)
	if e.status == http.StatusUnauthorized {
		w.Header().Set("WWW-Authenticate", "Bearer")
	}
	if e.code == "idempotency_in_progress" {
		w.Header().Set("Retry-After", "1")
	}
	fields := e.fields
	if fields == nil && e.status == http.StatusUnprocessableEntity {
		fields = []FieldError{{Code: e.code, Pointer: "", Detail: "Request content is invalid"}}
	}
	p := Problem{Type: "urn:kannon:problem:" + e.code, Title: http.StatusText(e.status), Status: e.status, Code: e.code, RequestId: w.Header().Get("X-Request-ID")}
	if len(fields) > 0 {
		p.Errors = &fields
	}
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(e.status)
	if err := json.NewEncoder(w).Encode(p); err != nil {
		slog.Debug("cannot write Problem Details", "err", err)
	}
}
