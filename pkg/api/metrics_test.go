package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"connectrpc.com/connect"
	"github.com/kannon-email/kannon/internal/admintoken"
	"github.com/kannon-email/kannon/internal/authzconnect"
	"github.com/kannon-email/kannon/pkg/api/apimetrics"
	adminv1 "github.com/kannon-email/kannon/proto/kannon/admin/apiv1"
	adminv1connect "github.com/kannon-email/kannon/proto/kannon/admin/apiv1/apiv1connect"
	mailerv1 "github.com/kannon-email/kannon/proto/kannon/mailer/apiv1"
	mailerv1connect "github.com/kannon-email/kannon/proto/kannon/mailer/apiv1/apiv1connect"
	"github.com/kannon-email/kannon/proto/kannon/stats/apiv1/apiv1connect"
	"github.com/kannon-email/kannon/proto/kannon/stats/apiv2/apiv2connect"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Every Connect handler is counted, health and the unauthenticated refusals included, by procedure
// and Connect code.
func TestEveryConnectCallIsCounted(t *testing.T) {
	reg := prometheus.NewRegistry()
	srv := metricsServer(t, reg, mailerv1connect.UnimplementedMailerHandler{})

	status, _ := postConnect(t, srv.URL+adminv1connect.ApiGetDomainsProcedure, marshal(t, &adminv1.GetDomainsReq{}), false)
	require.Equal(t, http.StatusUnauthorized, status)
	_, _ = postConnect(t, srv.URL+adminv1connect.HZServiceHZProcedure, marshal(t, &adminv1.HZRequest{}), false)

	want := `
# HELP kannon_api_requests_total Calls to the Connect and REST APIs, by operation and outcome.
# TYPE kannon_api_requests_total counter
kannon_api_requests_total{api="connect",code="unauthenticated",operation="/pkg.kannon.admin.apiv1.Api/GetDomains"} 1
kannon_api_requests_total{api="connect",code="unimplemented",operation="/pkg.kannon.admin.apiv1.HZService/HZ"} 1
`
	assert.NoError(t, testutil.GatherAndCompare(reg, strings.NewReader(want), "kannon_api_requests_total"))
}

// connect-go refuses some calls before any interceptor runs: a body over the read limit, one it
// cannot decode, a content type it does not speak, a procedure the service does not have. They are
// counted all the same, once, by procedure when it is one the service has and as unmatched when not.
func TestConnectCallsRefusedBeforeTheInterceptorsAreCountedOnce(t *testing.T) {
	reg := prometheus.NewRegistry()
	srv := metricsServer(t, reg, &countingMailer{})

	oversized := marshal(t, &mailerv1.SendHTMLReq{Html: strings.Repeat("a", connectReadMaxBytes(defaultMaxPayload))})
	status, _ := postConnect(t, srv.URL+mailerv1connect.MailerSendHTMLProcedure, oversized, false)
	require.Equal(t, http.StatusTooManyRequests, status)

	status, _ = postConnect(t, srv.URL+mailerv1connect.MailerSendHTMLProcedure, []byte{0xff, 0xff, 0xff}, false)
	require.Equal(t, http.StatusBadRequest, status)

	res, err := http.Post(srv.URL+mailerv1connect.MailerSendHTMLProcedure, "text/plain", strings.NewReader("hello"))
	require.NoError(t, err)
	_ = res.Body.Close()
	require.Equal(t, http.StatusUnsupportedMediaType, res.StatusCode)

	res, err = http.Post(srv.URL+"/pkg.kannon.mailer.apiv1.Mailer/SendToEveryone", "application/proto", strings.NewReader(""))
	require.NoError(t, err)
	_ = res.Body.Close()
	require.Equal(t, http.StatusNotFound, res.StatusCode)

	_, err = mailerv1connect.NewMailerClient(srv.Client(), srv.URL).SendHTML(t.Context(), connect.NewRequest(&mailerv1.SendHTMLReq{}))
	require.NoError(t, err)

	want := `
# HELP kannon_api_requests_total Calls to the Connect and REST APIs, by operation and outcome.
# TYPE kannon_api_requests_total counter
kannon_api_requests_total{api="connect",code="invalid_argument",operation="/pkg.kannon.mailer.apiv1.Mailer/SendHTML"} 1
kannon_api_requests_total{api="connect",code="ok",operation="/pkg.kannon.mailer.apiv1.Mailer/SendHTML"} 1
kannon_api_requests_total{api="connect",code="resource_exhausted",operation="/pkg.kannon.mailer.apiv1.Mailer/SendHTML"} 1
kannon_api_requests_total{api="connect",code="unimplemented",operation="unmatched"} 1
kannon_api_requests_total{api="connect",code="unknown",operation="/pkg.kannon.mailer.apiv1.Mailer/SendHTML"} 1
`
	assert.NoError(t, testutil.GatherAndCompare(reg, strings.NewReader(want), "kannon_api_requests_total"))
	n, err := testutil.GatherAndCount(reg, "kannon_api_request_duration_seconds")
	require.NoError(t, err)
	assert.Equal(t, 2, n, "one duration series per operation")
}

func metricsServer(t *testing.T, reg *prometheus.Registry, mailer mailerv1connect.MailerHandler) *httptest.Server {
	t.Helper()
	mux := newAPIMux(
		connectReadMaxBytes(defaultMaxPayload),
		authzconnect.AdminTokenHandlerOptions(admintoken.MustParse("secret")),
		apimetrics.New(reg),
		adminv1connect.UnimplementedApiHandler{},
		mailer,
		apiv1connect.UnimplementedStatsApiV1Handler{},
		apiv2connect.UnimplementedStatsApiV2Handler{},
		adminv1connect.UnimplementedHZServiceHandler{},
	)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}
