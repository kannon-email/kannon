package mailapi_test

import (
	"context"
	"database/sql"
	"encoding/base64"
	"os"
	"testing"

	"connectrpc.com/connect"
	schema "github.com/ludusrusso/kannon/db"
	sqlc "github.com/ludusrusso/kannon/internal/db"
	"github.com/ludusrusso/kannon/internal/tests"
	"github.com/ludusrusso/kannon/pkg/api/adminapi"
	"github.com/ludusrusso/kannon/pkg/api/mailapi"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"google.golang.org/grpc/metadata"

	adminv1 "github.com/ludusrusso/kannon/proto/kannon/admin/apiv1"

	adminv1cnt "github.com/ludusrusso/kannon/proto/kannon/admin/apiv1/apiv1connect"
	mailerv1cnt "github.com/ludusrusso/kannon/proto/kannon/mailer/apiv1/apiv1connect"

	_ "github.com/lib/pq"
)

var db *sql.DB
var q *sqlc.Queries
var ts mailerv1cnt.MailerHandler
var adminAPI adminv1cnt.ApiHandler

func TestMain(m *testing.M) {
	var purge tests.PurgeFunc
	var err error

	db, purge, err = tests.TestPostgresInit(schema.Schema)
	if err != nil {
		logrus.Fatalf("Could not start resource: %s", err)
	}

	q = sqlc.New(db)
	ts = mailapi.NewMailerAPIV1(q)
	adminAPI = adminapi.CreateAdminAPIService(q)

	code := m.Run()

	// You can't defer this because os.Exit doesn't care for defer
	if err := purge(); err != nil {
		logrus.Fatalf("Could not purge resource: %s", err)
	}

	os.Exit(code)
}

func createTestDomain(t *testing.T) *adminv1.Domain {
	t.Helper()
	res, err := adminAPI.CreateDomain(context.Background(), connect.NewRequest(&adminv1.CreateDomainRequest{
		Domain: "test.test.test",
	}))
	assert.Nil(t, err)
	return res.Msg
}

func cleanDB(t *testing.T) {
	t.Helper()
	_, err := db.ExecContext(context.Background(), "DELETE FROM domains")
	assert.Nil(t, err)

	_, err = db.ExecContext(context.Background(), "DELETE FROM sending_pool_emails")
	assert.Nil(t, err)

	_, err = db.ExecContext(context.Background(), "DELETE FROM templates")
	assert.Nil(t, err)
}

func getDomainCtx(d *adminv1.Domain) context.Context {
	token := base64.StdEncoding.EncodeToString([]byte(d.Domain + ":" + d.Key))
	ctx := metadata.NewIncomingContext(context.Background(), metadata.Pairs("authorization", "Basic "+token))
	return ctx
}
