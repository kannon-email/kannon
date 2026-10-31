// Package connecterr renders what an application operation returned as the Connect error a
// caller receives. It is the Connect half of internal/apperr: one Kind maps to one code here, and
// anything that is not a refusal is a fault.
package connecterr

import (
	"errors"
	"log/slog"

	"connectrpc.com/connect"
	"github.com/kannon-email/kannon/internal/apperr"
	"github.com/kannon-email/kannon/internal/authz"
)

var codes = map[apperr.Kind]connect.Code{
	apperr.Malformed: connect.CodeInvalidArgument,
	apperr.Invalid:   connect.CodeInvalidArgument,
	apperr.NotFound:  connect.CodeNotFound,
	apperr.Conflict:  connect.CodeAlreadyExists,
	apperr.Forbidden: connect.CodePermissionDenied,
}

// Error keeps err as the cause, so its text stays the message legacy clients already read. The
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
