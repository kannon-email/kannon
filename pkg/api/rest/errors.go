package rest

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/kannon-email/kannon/internal/apperr"
	"github.com/kannon-email/kannon/internal/intake"
)

// handler owns error responses. Operations write success only and must return
// before writing any response when they fail.
type handler func(http.ResponseWriter, *http.Request) error

func (h handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if err := h(w, r); err != nil {
		writeError(w, r, err)
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

// statuses maps each application Kind to its REST status; the Code travels unchanged.
var statuses = map[apperr.Kind]int{
	apperr.Malformed: http.StatusBadRequest,
	apperr.Invalid:   http.StatusUnprocessableEntity,
	apperr.NotFound:  http.StatusNotFound,
	apperr.Conflict:  http.StatusConflict,
	apperr.Forbidden: http.StatusForbidden,
}

func errorProblem(err error) *requestError {
	var request *requestError
	if errors.As(err, &request) {
		return request
	}
	var limit *intake.FieldsLimitError
	if errors.As(err, &limit) {
		return fieldsLimitProblem(limit)
	}
	if e, ok := apperr.As(err); ok {
		if status, ok := statuses[e.Kind]; ok {
			return problemError(status, e.Code)
		}
	}
	return problemError(http.StatusInternalServerError, "internal_error")
}

// fieldsLimitProblem names the limit a refused Batch exceeded and the fields to shrink, so a
// caller can size the next request.
func fieldsLimitProblem(e *intake.FieldsLimitError) *requestError {
	pointer := "/global_fields"
	if e.Recipient != intake.GlobalFields {
		pointer = fmt.Sprintf("/recipients/%d/fields", e.Recipient)
	}
	return &requestError{status: statuses[e.Limit.Kind], code: e.Limit.Code, fields: []FieldError{{Code: e.Limit.Code, Pointer: pointer, Detail: e.Error()}}}
}

func writeError(w http.ResponseWriter, r *http.Request, err error) {
	e := errorProblem(err)
	logError(r, w.Header().Get("X-Request-ID"), e.status, err)
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

// logError records what the Problem leaves out: who failed to authenticate, never with what,
// and the cause of a 500, under the request ID the caller was given.
func logError(r *http.Request, requestID string, status int, err error) {
	switch {
	case status == http.StatusUnauthorized:
		slog.Warn("REST authentication failed", "peer", r.RemoteAddr, "path", versionedPath(r, r.URL.Path), "request_id", requestID)
	case status >= http.StatusInternalServerError:
		slog.Error("REST request failed", "method", r.Method, "path", versionedPath(r, r.URL.Path), "request_id", requestID, "err", err)
	}
}
