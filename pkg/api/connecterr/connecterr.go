// Package connecterr renders what an application operation returned as the Connect error a
// caller receives. It is the Connect half of internal/apperr: one Kind maps to one code here, and
// anything that is not a refusal is a fault.
package connecterr

import (
	"context"
	"errors"
	"log/slog"

	"connectrpc.com/connect"
	"github.com/kannon-email/kannon/internal/apperr"
	"github.com/kannon-email/kannon/internal/authz"
	"github.com/kannon-email/kannon/pkg/api/requestid"
)

var codes = map[apperr.Kind]connect.Code{
	apperr.Malformed: connect.CodeInvalidArgument,
	apperr.Invalid:   connect.CodeInvalidArgument,
	apperr.NotFound:  connect.CodeNotFound,
	apperr.Conflict:  connect.CodeAlreadyExists,
	apperr.Forbidden: connect.CodePermissionDenied,
}

// Error keeps err as the cause, so a refusal's text stays the message legacy clients already
// read; a fault's text is for the log, and Interceptor keeps it from the caller. The
// two authorization sentinels are logged apart — a credential that may not, versus nothing having
// authenticated the request.
func Error(err error) *connect.Error {
	switch {
	case errors.Is(err, authz.ErrNoPrincipal):
		slog.Warn("authorization refused: nothing authenticated this request", "err", err)
	case errors.Is(err, authz.ErrForbidden):
		slog.Warn("authorization refused: this Principal may not do this", "err", err)
	}
	if e, ok := apperr.As(err); ok {
		if code, ok := codes[e.Kind]; ok {
			return connect.NewError(code, err)
		}
	}
	return connect.NewError(connect.CodeInternal, err)
}

// Interceptor keeps faults from reaching the caller: an internal or unknown error, or one that is
// not a Connect error at all, becomes a generic internal error carrying a request ID in its
// metadata, and its cause is logged under that ID.
func Interceptor() connect.Interceptor {
	return connect.UnaryInterceptorFunc(func(next connect.UnaryFunc) connect.UnaryFunc {
		return func(ctx context.Context, req connect.AnyRequest) (connect.AnyResponse, error) {
			res, err := next(ctx, req)
			if err == nil || !isFault(err) {
				return res, err
			}
			id := requestid.New()
			slog.Error("Connect request failed", "procedure", req.Spec().Procedure, "request_id", id, "err", err)
			generic := connect.NewError(connect.CodeInternal, errors.New("internal"))
			generic.Meta().Set(requestid.Header, id)
			return nil, generic
		}
	})
}

func isFault(err error) bool {
	var ce *connect.Error
	if !errors.As(err, &ce) {
		return true
	}
	return ce.Code() == connect.CodeInternal || ce.Code() == connect.CodeUnknown
}
