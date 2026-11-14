package rest_test

import (
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"connectrpc.com/connect"
	"github.com/kannon-email/kannon/internal/admintoken"
	"github.com/kannon-email/kannon/internal/apikeys"
	"github.com/kannon-email/kannon/internal/authz"
	sqlc "github.com/kannon-email/kannon/internal/db"
	"github.com/kannon-email/kannon/internal/domains"
	"github.com/kannon-email/kannon/internal/intake"
	"github.com/kannon-email/kannon/internal/values"
	"github.com/kannon-email/kannon/pkg/api/connecterr"
	"github.com/kannon-email/kannon/pkg/api/mailapi"
	"github.com/kannon-email/kannon/pkg/api/rest"
	mailerv1 "github.com/kannon-email/kannon/proto/kannon/mailer/apiv1"
	"github.com/kannon-email/kannon/proto/kannon/mailer/apiv1/apiv1connect"
	mailertypes "github.com/kannon-email/kannon/proto/kannon/mailer/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A NUL byte reached Postgres, which cannot store it in text: REST answered an unlogged 500 and
// the legacy API handed the caller the database's error. Both now refuse it as the caller's
// mistake, before anything is written.
func TestANULByteIsRefusedBeforeItReachesTheDatabase(t *testing.T) {
	if testing.Short() {
		t.Skip("requires Docker PostgreSQL")
	}
	db, purge, err := credentialDB()
	require.NoError(t, err)
	t.Cleanup(func() {
		db.Close()
		assert.NoError(t, purge())
	})
	ctx := authz.NewContext(t.Context(), admintoken.AdminPrincipal())
	d := values.MustParse("a.example.com")
	_, err = domains.NewService(sqlc.NewDomainsRepository(db)).CreateDomain(ctx, d)
	require.NoError(t, err)

	t.Run("REST", func(t *testing.T) {
		logs := captureLogs(t)
		h := rest.NewHandler(rest.Services{
			Domains: domains.NewService(sqlc.NewDomainsRepository(db)), Token: admintoken.MustParse(testAdminToken),
			Inline: intake.NewInlineService(sqlc.NewInlineStore(db), sqlc.NewDomainsRepository(db), nil, 0, 500<<10),
		})
		for _, body := range []string{
			`{"from":{"email":"n@a.example.com"},"subject":"a\u0000b","content":{"type":"html","html":"x"},"recipients":[{"email":"r@x.org"}]}`,
			`{"from":{"email":"n@a.example.com"},"subject":"hi","content":{"type":"html","html":"x"},"recipients":[{"email":"r@x.org","fields":{"name":"a\u0000"}}]}`,
		} {
			r := httptest.NewRequest(http.MethodPost, "/v1/domains/a.example.com/batches", strings.NewReader(body))
			r.Header.Set("Authorization", "Bearer "+testAdminToken)
			r.Header.Set("Idempotency-Key", "nul-"+base64.RawURLEncoding.EncodeToString([]byte(body))[:32])
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)

			assert.Equal(t, http.StatusUnprocessableEntity, w.Code, w.Body.String())
			assert.Contains(t, w.Body.String(), `"code":"validation_failed"`)
		}
		for _, line := range logs() {
			assert.NotEqual(t, "ERROR", line["level"], line)
		}
	})

	t.Run("Connect", func(t *testing.T) {
		k, err := apikeys.NewService(sqlc.NewAPIKeysRepository(db)).CreateKey(ctx, d, "s", nil)
		require.NoError(t, err)
		mux := http.NewServeMux()
		mux.Handle(apiv1connect.NewMailerHandler(mailapi.NewMailerAPIV1(db, nil, 0, 500<<10, nil), connect.WithInterceptors(connecterr.Interceptor())))
		srv := httptest.NewServer(mux)
		t.Cleanup(srv.Close)
		req := connect.NewRequest(&mailerv1.SendHTMLReq{
			Sender:     &mailertypes.Sender{Email: "n@a.example.com", Alias: "x"},
			Subject:    "hi\x00there",
			Html:       "<p>x</p>",
			Recipients: []*mailertypes.Recipient{{Email: "r@x.org"}},
		})
		req.Header().Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte("a.example.com:"+k.PlaintextKey)))
		_, err = apiv1connect.NewMailerClient(srv.Client(), srv.URL).SendHTML(t.Context(), req)

		assert.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err))
		assert.NotContains(t, err.Error(), "UTF8")
		assert.NotContains(t, err.Error(), "SQLSTATE")
	})
}
