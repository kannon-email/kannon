// Package authzconnect is where the authority model meets Connect: it reads the credential a
// request carries, puts the Principal that credential resolves to into the request's context, and
// turns a refusal into the status code a caller receives. Its own package so that internal/authz
// cannot import a transport and its decision stays pure.
package authzconnect

import (
	"context"
	"errors"
	"log/slog"

	"connectrpc.com/connect"
	"github.com/kannon-email/kannon/internal/admintoken"
	"github.com/kannon-email/kannon/internal/authz"
)

// AdminTokenHeader is where the admin credential travels. A header of Kannon's own rather than
// Authorization, which the Mailer API already spends on HTTP Basic `<domain>:<key>`: one header
// carrying two credential schemes is a proxy rule or an access-log filter away from a send key
// being read as an admin token. Declared here, so the handler that reads it and the client that
// sets it cannot come to spell it differently.
const AdminTokenHeader = "X-Kannon-Admin-Token"

// AttributionHeader is where a claim about who asked travels: a front-end holding the admin token
// serves its own people and hands their requests on, and this is how it names one (ADR 0009). A
// second header rather than more of the credential's, because it is not a credential — it is
// unverifiable, it confers nothing, and a request carrying it is authenticated exactly as far as
// a request without it. What it requires is the attribute Action, which Guard asks for.
const AttributionHeader = "X-Kannon-Attribution"

// AdminTokenHandlerOptions returns the Connect options that authenticate a request with the
// operator's admin token and install the Principal it resolves to. Never give these to the mailer
// handler, which authenticates its own sender credential, nor to health, which discloses nothing.
func AdminTokenHandlerOptions(t admintoken.Token) []connect.HandlerOption {
	return []connect.HandlerOption{connect.WithInterceptors(adminTokenInterceptor(t))}
}

// AdminTokenClientOptions is the other side of the same header, for a client calling the Admin or
// Stats APIs. Given so that no caller writes the header name itself: one that did would keep
// compiling after the name changed here and start failing authentication instead.
func AdminTokenClientOptions(token string) []connect.ClientOption {
	return []connect.ClientOption{connect.WithInterceptors(sendAdminTokenInterceptor(token))}
}

// AttributionClientOptions is the client side of the attribution header, for a front-end calling
// the Admin or Stats APIs in one of its own users' names. Separate from the token's options
// because the two are independent: the credential says who may act, the claim says who asked.
func AttributionClientOptions(attribution string) []connect.ClientOption {
	return []connect.ClientOption{connect.WithInterceptors(sendAttributionInterceptor(attribution))}
}

// adminTokenInterceptor authenticates every unary request it sees and refuses the ones it cannot.
// Refusing here rather than installing nothing and leaving Guard to answer keeps the two apart for
// the caller: unauthenticated says the credential was wrong or absent, permission denied says a
// resolved Principal may not do this. Unary only: a guarded operation reached over a stream finds
// no Principal and refuses, which is the direction to fail in, and every procedure on these
// surfaces is unary today.
func adminTokenInterceptor(t admintoken.Token) connect.Interceptor {
	return connect.UnaryInterceptorFunc(func(next connect.UnaryFunc) connect.UnaryFunc {
		return func(ctx context.Context, req connect.AnyRequest) (connect.AnyResponse, error) {
			p, err := t.Authenticate(req.Header().Get(AdminTokenHeader))
			if err != nil {
				slog.Warn("authentication refused: the admin token was absent or wrong",
					"procedure", req.Spec().Procedure, "peer", req.Peer().Addr)
				return nil, connect.NewError(connect.CodeUnauthenticated, err)
			}

			p, err = attributed(p, req.Header().Get(AttributionHeader))
			if err != nil {
				slog.Warn("attribution refused: the claim is malformed",
					"procedure", req.Spec().Procedure, "err", err)
				return nil, connect.NewError(connect.CodeInvalidArgument, err)
			}

			return next(authz.NewContext(ctx, p), req)
		}
	})
}

// attributed applies the claim a request carries, if it carries one. An empty header is no claim
// rather than a claim naming nobody: a caller that has nobody to name sends the header unset, and
// most client libraries cannot tell the two apart anyway.
//
// A malformed claim is refused, not dropped: the header exists to put a name in a record, so a
// front-end whose claim was discarded would go on believing one was recorded. Invalid argument and
// not permission denied — the credential is fine and the operation is permitted; what arrived
// wrong is the claim, which is the caller's to fix.
func attributed(p authz.Principal, header string) (authz.Principal, error) {
	if header == "" {
		return p, nil
	}
	a, err := authz.ParseAttribution(header)
	if err != nil {
		return p, err
	}
	return p.WithAttribution(a), nil
}

// sendAttributionInterceptor sets the attribution header on outgoing calls, guarded on IsClient
// for the same reason the token's is: one type serves both directions, and a value of this one
// reaching a handler would let a request name whoever it liked on the way in.
func sendAttributionInterceptor(attribution string) connect.Interceptor {
	return connect.UnaryInterceptorFunc(func(next connect.UnaryFunc) connect.UnaryFunc {
		return func(ctx context.Context, req connect.AnyRequest) (connect.AnyResponse, error) {
			if req.Spec().IsClient {
				req.Header().Set(AttributionHeader, attribution)
			}
			return next(ctx, req)
		}
	})
}

// sendAdminTokenInterceptor sets the header on outgoing calls. Guarded on IsClient because
// connect.Interceptor is one type serving both directions, and a value of this one reaching a
// handler would let a caller hand itself the token the handler is about to check.
func sendAdminTokenInterceptor(token string) connect.Interceptor {
	return connect.UnaryInterceptorFunc(func(next connect.UnaryFunc) connect.UnaryFunc {
		return func(ctx context.Context, req connect.AnyRequest) (connect.AnyResponse, error) {
			if req.Spec().IsClient {
				req.Header().Set(AdminTokenHeader, token)
			}
			return next(ctx, req)
		}
	})
}

// Error renders a service-layer error as the Connect error a caller receives: both authorization
// sentinels become CodePermissionDenied, anything else the caller's own fallback. The two are
// logged apart — a credential that may not, versus nothing having authenticated the request.
func Error(err error, fallback connect.Code) *connect.Error {
	switch {
	case errors.Is(err, authz.ErrNoPrincipal):
		slog.Warn("authorization refused: nothing authenticated this request", "err", err)
		return connect.NewError(connect.CodePermissionDenied, err)
	case errors.Is(err, authz.ErrForbidden):
		slog.Warn("authorization refused: this Principal may not do this", "err", err)
		return connect.NewError(connect.CodePermissionDenied, err)
	default:
		return connect.NewError(fallback, err)
	}
}
