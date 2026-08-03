package authzconnect_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"connectrpc.com/connect"
	"github.com/kannon-email/kannon/internal/admintoken"
	"github.com/kannon-email/kannon/internal/authz"
	"github.com/kannon-email/kannon/internal/authzconnect"
	pb "github.com/kannon-email/kannon/proto/kannon/admin/apiv1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// procedure is a made-up RPC path. The interceptor is indifferent to which service it is mounted on
// — ADR 0008 rejects deriving authority from a method name — so a probe is used rather than one of
// the real handlers, which would need a database to say anything.
const procedure = "/kannon.test.authzconnect.Probe/Call"

// serverSecret is what the guarded probe below is configured with. A test presenting
// something else is presenting a wrong token, which is the case that matters.
const serverSecret = "s3cr3t-admin-token"

// The whole of what the header does, end to end through a real Connect server: with the token, a
// request reaches the operation; without it, or with the wrong one, it is refused as
// unauthenticated and the operation never runs. The assertion on `reached` is what rules out a
// surface that ran it and discarded the answer.
func TestAdminTokenHandlerOptions(t *testing.T) {
	tests := []struct {
		name      string
		presented string
		setHeader bool
		wantCode  connect.Code
	}{
		{
			name:      "the configured token: the request reaches the operation",
			presented: serverSecret,
			setHeader: true,
		},
		{
			name:      "a wrong token: refused before the operation runs",
			presented: "not-the-token",
			setHeader: true,
			wantCode:  connect.CodeUnauthenticated,
		},
		{
			name:      "an empty header",
			presented: "",
			setHeader: true,
			wantCode:  connect.CodeUnauthenticated,
		},
		{
			// The shape of every request sent to these surfaces before this
			// change, which used to be served as admin on everything.
			name:      "no header at all",
			setHeader: false,
			wantCode:  connect.CodeUnauthenticated,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var reached bool
			var sawPrincipal authz.Principal

			handler := connect.NewUnaryHandler(procedure,
				func(ctx context.Context, _ *connect.Request[pb.GetDomainsReq]) (*connect.Response[pb.GetDomainsResponse], error) {
					// Guarded exactly as a real operation is, so that what is being
					// tested is the pair — an installed Principal and a guard that
					// consults it — rather than the interceptor in isolation.
					_, err := authz.Guard(ctx, authz.Create, authz.Domains(), func() (struct{}, error) {
						reached = true
						sawPrincipal, _ = authz.FromContext(ctx)
						return struct{}{}, nil
					})
					if err != nil {
						return nil, authzconnect.Error(err, connect.CodeInternal)
					}
					return connect.NewResponse(&pb.GetDomainsResponse{}), nil
				},
				authzconnect.AdminTokenHandlerOptions(admintoken.MustParse(serverSecret))...)

			mux := http.NewServeMux()
			mux.Handle(procedure, handler)
			server := httptest.NewServer(mux)
			t.Cleanup(server.Close)

			client := connect.NewClient[pb.GetDomainsReq, pb.GetDomainsResponse](server.Client(), server.URL+procedure)
			req := connect.NewRequest(&pb.GetDomainsReq{})
			if tc.setHeader {
				req.Header().Set(authzconnect.AdminTokenHeader, tc.presented)
			}
			_, err := client.CallUnary(t.Context(), req)

			assert.Equal(t, tc.wantCode == 0, reached)
			if tc.wantCode == 0 {
				require.NoError(t, err)
				// The identifier names the credential, not the authority: what is
				// recorded of an operation is that a holder of the admin token did
				// it, since the token cannot say which holder.
				assert.Equal(t, "admin-token", sawPrincipal.ID())
				return
			}
			assert.Equal(t, tc.wantCode, connect.CodeOf(err))
		})
	}
}

// The client option is the handler option's counterpart: a client given the token authenticates
// without its caller ever naming the header. Asserted through the same guarded probe, because the
// property owed here is that the two agree, and only a round trip can show that.
func TestAdminTokenClientOptionsAuthenticate(t *testing.T) {
	var sawPrincipal authz.Principal

	handler := connect.NewUnaryHandler(procedure,
		func(ctx context.Context, _ *connect.Request[pb.GetDomainsReq]) (*connect.Response[pb.GetDomainsResponse], error) {
			_, err := authz.Guard(ctx, authz.Create, authz.Domains(), func() (struct{}, error) {
				sawPrincipal, _ = authz.FromContext(ctx)
				return struct{}{}, nil
			})
			if err != nil {
				return nil, authzconnect.Error(err, connect.CodeInternal)
			}
			return connect.NewResponse(&pb.GetDomainsResponse{}), nil
		},
		authzconnect.AdminTokenHandlerOptions(admintoken.MustParse(serverSecret))...)

	mux := http.NewServeMux()
	mux.Handle(procedure, handler)
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

	client := connect.NewClient[pb.GetDomainsReq, pb.GetDomainsResponse](server.Client(), server.URL+procedure,
		authzconnect.AdminTokenClientOptions(serverSecret)...)
	_, err := client.CallUnary(t.Context(), connect.NewRequest(&pb.GetDomainsReq{}))

	require.NoError(t, err)
	assert.Equal(t, "admin-token", sawPrincipal.ID())
}

// Both refusals reach the caller as permission denied and both stay distinguishable behind it. The
// code is what a client acts on; the sentinel is what a log reads to tell "this credential may not"
// from "nothing authenticated this request", which are very different problems for an operator.
func TestErrorMapsRefusalsToPermissionDenied(t *testing.T) {
	other := errors.New("the database is on fire")

	tests := []struct {
		name     string
		err      error
		wantCode connect.Code
	}{
		{
			name:     "a Principal that may not do this",
			err:      authz.ErrForbidden,
			wantCode: connect.CodePermissionDenied,
		},
		{
			name:     "nothing authenticated the request",
			err:      authz.ErrNoPrincipal,
			wantCode: connect.CodePermissionDenied,
		},
		{
			// Wrapped, because a service returns a refusal from several frames down and
			// the mapping must not depend on it arriving bare.
			name:     "a refusal wrapped on its way out",
			err:      errors.Join(errors.New("update template"), authz.ErrForbidden),
			wantCode: connect.CodePermissionDenied,
		},
		{
			name:     "anything else keeps the fallback it always had",
			err:      other,
			wantCode: connect.CodeInternal,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := authzconnect.Error(tc.err, connect.CodeInternal)

			assert.Equal(t, tc.wantCode, got.Code())
			assert.ErrorIs(t, got, tc.err, "the cause has to survive into the Connect error")
		})
	}
}
