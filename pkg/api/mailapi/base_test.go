package mailapi_test

import (
	"encoding/base64"
	"log/slog"
	"os"
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
	var purge tests.PurgeFunc
	var err error

	db, purge, err = tests.TestPostgresInit(schema.Schema)
	if err != nil {
		slog.Error("Could not start resource", "err", err)
		os.Exit(1)
	}

	q = sqlc.New(db)
	ts = mailapi.NewMailerAPIV1(db, delivery.DefaultBackoff, delivery.DefaultRetryWindow)
	adminAPI = adminapi.CreateAdminAPIService(db)

	code := m.Run()

	// You can't defer this because os.Exit doesn't care for defer
	if err := purge(); err != nil {
		slog.Error("Could not purge resource", "err", err)
		os.Exit(1)
	}

	os.Exit(code)
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

	// Before templates: a Batch holds a key on the Template it sends (ADR 0008),
	// so a Template still referenced by one of these rows cannot be deleted.
	_, err = db.Exec(t.Context(), "DELETE FROM messages")
	assert.Nil(t, err)

	_, err = db.Exec(t.Context(), "DELETE FROM templates")
	assert.Nil(t, err)
}

func authRequest[T any](req *connect.Request[T], d *tests.DomainWithKey) {
	token := base64.StdEncoding.EncodeToString([]byte(d.Domain.Domain + ":" + d.APIKey))
	req.Header().Set("Authorization", "Basic "+token)
}
