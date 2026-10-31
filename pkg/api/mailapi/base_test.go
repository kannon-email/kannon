package mailapi_test

import (
	"encoding/base64"
	"log/slog"
	"testing"

	"connectrpc.com/connect"
	"github.com/jackc/pgx/v5/pgxpool"
	schema "github.com/kannon-email/kannon/db"
	sqlc "github.com/kannon-email/kannon/internal/db"
	"github.com/kannon-email/kannon/internal/delivery"
	"github.com/kannon-email/kannon/internal/tests"
	"github.com/kannon-email/kannon/pkg/api/adminapi"
	"github.com/kannon-email/kannon/pkg/api/mailapi"
	"github.com/stretchr/testify/assert"

	adminv1connect "github.com/kannon-email/kannon/proto/kannon/admin/apiv1/apiv1connect"
	mailerv1connect "github.com/kannon-email/kannon/proto/kannon/mailer/apiv1/apiv1connect"
)

var db *pgxpool.Pool
var q *sqlc.Queries
var ts mailerv1connect.MailerHandler
var adminAPI adminv1connect.ApiHandler

func TestMain(m *testing.M) {
	tests.Main(func() int {
		var purge tests.PurgeFunc
		var err error

		db, purge, err = tests.TestPostgresInit(schema.Schema)
		if err != nil {
			slog.Error("Could not start resource", "err", err)
			return 1
		}

		q = sqlc.New(db)
		ts = mailapi.NewMailerAPIV1(db, delivery.DefaultBackoff, delivery.DefaultRetryWindow, 0)
		adminAPI = adminapi.CreateAdminAPIService(db)

		code := m.Run()

		if err := purge(); err != nil {
			slog.Error("Could not purge resource", "err", err)
			return 1
		}

		return code
	})
}

func createTestDomain(t *testing.T) *tests.DomainWithKey {
	t.Helper()
	return tests.CreateTestDomain(t, adminAPI)
}

func cleanDB(t *testing.T) {
	t.Helper()
	_, err := db.Exec(t.Context(), "DELETE FROM domains")
	assert.Nil(t, err)

	_, err = db.Exec(t.Context(), "DELETE FROM sending_pool_emails")
	assert.Nil(t, err)

	_, err = db.Exec(t.Context(), "DELETE FROM templates")
	assert.Nil(t, err)
}

func authRequest[T any](req *connect.Request[T], d *tests.DomainWithKey) {
	token := base64.StdEncoding.EncodeToString([]byte(d.Domain.Domain + ":" + d.APIKey))
	req.Header().Set("Authorization", "Basic "+token)
}
