package mailapi_test

import (
	"context"
	"encoding/base64"
	"os"
	"testing"

	"connectrpc.com/connect"
	"github.com/jackc/pgx/v5/pgxpool"
	schema "github.com/kannon-email/kannon/db"
	sqlc "github.com/kannon-email/kannon/internal/db"
	"github.com/kannon-email/kannon/internal/tests"
	"github.com/kannon-email/kannon/pkg/api/adminapi"
	"github.com/kannon-email/kannon/pkg/api/mailapi"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"

	adminv1connect "github.com/kannon-email/kannon/proto/kannon/admin/apiv1/apiv1connect"
	mailerv1connect "github.com/kannon-email/kannon/proto/kannon/mailer/apiv1/apiv1connect"

	adminv1 "github.com/kannon-email/kannon/proto/kannon/admin/apiv1"

	_ "github.com/lib/pq"
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
		logrus.Fatalf("Could not start resource: %s", err)
	}

	q = sqlc.New(db)
	ts = mailapi.NewMailerAPIV1(q, db)
	adminAPI = adminapi.CreateAdminAPIService(q, db)

	code := m.Run()

	// You can't defer this because os.Exit doesn't care for defer
	if err := purge(); err != nil {
		logrus.Fatalf("Could not purge resource: %s", err)
	}

	os.Exit(code)
}

type testDomainWithKey struct {
	domain *adminv1.Domain
	apiKey string
}

func createTestDomain(t *testing.T) *testDomainWithKey {
	t.Helper()
	res, err := adminAPI.CreateDomain(context.Background(), connect.NewRequest(&adminv1.CreateDomainRequest{
		Domain: "test.test.test",
	}))
	assert.Nil(t, err)

	// Create an API key for authentication
	keyRes, err := adminAPI.CreateAPIKey(context.Background(), connect.NewRequest(&adminv1.CreateAPIKeyRequest{
		Domain: res.Msg.Domain,
		Name:   "test-key",
	}))
	assert.Nil(t, err)

	return &testDomainWithKey{
		domain: res.Msg,
		apiKey: keyRes.Msg.ApiKey.Key,
	}
}

func cleanDB(t *testing.T) {
	t.Helper()
	_, err := db.Exec(context.Background(), "DELETE FROM domains")
	assert.Nil(t, err)

	_, err = db.Exec(context.Background(), "DELETE FROM sending_pool_emails")
	assert.Nil(t, err)

	_, err = db.Exec(context.Background(), "DELETE FROM templates")
	assert.Nil(t, err)
}

func authRequest[T any](req *connect.Request[T], d *testDomainWithKey) {
	token := base64.StdEncoding.EncodeToString([]byte(d.domain.Domain + ":" + d.apiKey))
	req.Header().Set("Authorization", "Basic "+token)
}
