package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/kannon-email/kannon/internal/admintoken"
	"github.com/kannon-email/kannon/internal/authzconnect"
	"github.com/kannon-email/kannon/pkg/api/apimetrics"
	adminv1 "github.com/kannon-email/kannon/proto/kannon/admin/apiv1"
	adminv1connect "github.com/kannon-email/kannon/proto/kannon/admin/apiv1/apiv1connect"
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
	mux := newAPIMux(
		connectReadMaxBytes(defaultMaxPayload),
		authzconnect.AdminTokenHandlerOptions(admintoken.MustParse("secret")),
		apimetrics.New(reg),
		adminv1connect.UnimplementedApiHandler{},
		mailerv1connect.UnimplementedMailerHandler{},
		apiv1connect.UnimplementedStatsApiV1Handler{},
		apiv2connect.UnimplementedStatsApiV2Handler{},
		adminv1connect.UnimplementedHZServiceHandler{},
	)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

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
