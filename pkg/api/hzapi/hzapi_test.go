package hzapi_test

import (
	"bytes"
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/kannon-email/kannon/pkg/api/hzapi"
	pb "github.com/kannon-email/kannon/proto/kannon/admin/apiv1"
	"github.com/kannon-email/kannon/proto/kannon/admin/apiv1/apiv1connect"
	"github.com/kannon-email/kannon/x/config"
	"github.com/kannon-email/kannon/x/container"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The health service answers anyone, so a database it cannot reach must not hand the caller the
// driver's error, which names the database user, the database and the host. The operator finds
// that detail in the log instead.
func TestHealthReportsOnlyStatusWhenTheDatabaseIsDown(t *testing.T) {
	var logs bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })

	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	cnt := container.New(ctx, config.RootConfig{DatabaseURL: "postgres://kannon_svc:hunter2@127.0.0.1:1/kannon_prod?sslmode=disable&connect_timeout=2"})
	_ = cnt.DB()
	mux := http.NewServeMux()
	mux.Handle(apiv1connect.NewHZServiceHandler(hzapi.CreateHZAPIService(cnt)))
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	res, err := apiv1connect.NewHZServiceClient(srv.Client(), srv.URL).HZ(ctx, connect.NewRequest(&pb.HZRequest{}))
	require.NoError(t, err)

	require.NotEmpty(t, res.Msg.Result)
	for name, status := range res.Msg.Result {
		assert.Equal(t, "unavailable", status, name)
	}
	assert.Contains(t, logs.String(), "127.0.0.1")
	for _, secret := range []string{"kannon_svc", "kannon_prod", "127.0.0.1", "hunter2"} {
		for _, status := range res.Msg.Result {
			assert.NotContains(t, status, secret)
		}
	}
	assert.NotContains(t, logs.String(), "hunter2")
}
