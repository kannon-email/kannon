package api

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"connectrpc.com/connect"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/kannon-email/kannon/internal/admintoken"
	"github.com/kannon-email/kannon/internal/authzconnect"
	"github.com/kannon-email/kannon/internal/domains"
	"github.com/kannon-email/kannon/pkg/api/apimetrics"
	"github.com/kannon-email/kannon/pkg/api/connecterr"
	adminv1 "github.com/kannon-email/kannon/proto/kannon/admin/apiv1"
	adminv1connect "github.com/kannon-email/kannon/proto/kannon/admin/apiv1/apiv1connect"
	mailerv1connect "github.com/kannon-email/kannon/proto/kannon/mailer/apiv1/apiv1connect"
	"github.com/kannon-email/kannon/proto/kannon/stats/apiv1/apiv1connect"
	"github.com/kannon-email/kannon/proto/kannon/stats/apiv2/apiv2connect"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const connectTestToken = "0123456789abcdef0123456789abcdef"

// failingAdmin answers CreateDomain with whatever err it holds, the way a handler does when its
// storage fails.
type failingAdmin struct {
	adminv1connect.UnimplementedApiHandler
	err error
}

func (a failingAdmin) CreateDomain(context.Context, *connect.Request[adminv1.CreateDomainRequest]) (*connect.Response[adminv1.Domain], error) {
	return nil, a.err
}

// A fault in a Connect handler used to reach the caller as the database's own words. The caller
// now gets a generic message and a request ID, and the log has the cause under that ID.
func TestAConnectFaultIsGenericForTheCallerAndLoggedWithItsRequestID(t *testing.T) {
	pgErr := &pgconn.PgError{Code: "22021", Message: "invalid byte sequence for encoding \"UTF8\": 0x00", Detail: "secret data"}
	for _, tc := range []struct {
		name string
		err  error
	}{
		{"application fault", connecterr.Error(pgErr)},
		{"raw error", pgErr},
		{"internal Connect error", connect.NewError(connect.CodeInternal, pgErr)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var logs bytes.Buffer
			prev := slog.Default()
			slog.SetDefault(slog.New(slog.NewJSONHandler(&logs, nil)))
			t.Cleanup(func() { slog.SetDefault(prev) })

			srv := httptest.NewServer(connectMux(failingAdmin{err: tc.err}))
			t.Cleanup(srv.Close)
			client := adminv1connect.NewApiClient(srv.Client(), srv.URL)
			req := connect.NewRequest(&adminv1.CreateDomainRequest{Domain: "example.com"})
			req.Header().Set(authzconnect.AdminTokenHeader, connectTestToken)
			_, err := client.CreateDomain(t.Context(), req)

			var ce *connect.Error
			require.ErrorAs(t, err, &ce)
			assert.Equal(t, connect.CodeInternal, ce.Code())
			assert.Equal(t, "internal", ce.Message())
			id := ce.Meta().Get("X-Request-ID")
			require.NotEmpty(t, id)

			var logged []map[string]any
			for _, raw := range strings.Split(strings.TrimSpace(logs.String()), "\n") {
				var line map[string]any
				require.NoError(t, json.Unmarshal([]byte(raw), &line))
				if line["request_id"] == id {
					logged = append(logged, line)
				}
			}
			require.Len(t, logged, 1)
			assert.Equal(t, "ERROR", logged[0]["level"])
			assert.Equal(t, adminv1connect.ApiCreateDomainProcedure, logged[0]["procedure"])
			assert.Contains(t, logged[0]["err"], "invalid byte sequence")
		})
	}
}

// A refusal is the caller's to act on, so its code and message still reach it.
func TestAConnectRefusalKeepsItsMessage(t *testing.T) {
	srv := httptest.NewServer(connectMux(failingAdmin{err: connecterr.Error(domains.ErrDomainAlreadyExists)}))
	t.Cleanup(srv.Close)
	req := connect.NewRequest(&adminv1.CreateDomainRequest{Domain: "example.com"})
	req.Header().Set(authzconnect.AdminTokenHeader, connectTestToken)
	_, err := adminv1connect.NewApiClient(srv.Client(), srv.URL).CreateDomain(t.Context(), req)

	assert.Equal(t, connect.CodeAlreadyExists, connect.CodeOf(err))
	assert.ErrorContains(t, err, domains.ErrDomainAlreadyExists.Error())
}

func connectMux(admin adminv1connect.ApiHandler) *http.ServeMux {
	return newAPIMux(
		connectReadMaxBytes(defaultMaxPayload),
		authzconnect.AdminTokenHandlerOptions(admintoken.MustParse(connectTestToken)),
		apimetrics.New(prometheus.NewRegistry()),
		admin,
		mailerv1connect.UnimplementedMailerHandler{},
		apiv1connect.UnimplementedStatsApiV1Handler{},
		apiv2connect.UnimplementedStatsApiV2Handler{},
		adminv1connect.UnimplementedHZServiceHandler{},
	)
}
