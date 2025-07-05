package mailapi_test

import (
	"context"
	"encoding/base64"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	schema "github.com/ludusrusso/kannon/db"
	sqlc "github.com/ludusrusso/kannon/internal/db"
	"github.com/ludusrusso/kannon/internal/tests"
	"github.com/ludusrusso/kannon/pkg/api/adminapi"
	"github.com/ludusrusso/kannon/pkg/api/mailapi"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"google.golang.org/grpc/metadata"

	adminv1 "github.com/ludusrusso/kannon/proto/kannon/admin/apiv1"
	mailerv1 "github.com/ludusrusso/kannon/proto/kannon/mailer/apiv1"

	_ "github.com/lib/pq"
)

var db *pgxpool.Pool
var q *sqlc.Queries
var ts mailerv1.MailerServer
var adminAPI adminv1.ApiServer

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
	res, err := adminAPI.CreateDomain(context.Background(), &adminv1.CreateDomainRequest{
		Domain: "test.test.test",
	})
	assert.Nil(t, err)
	return res
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

func getDomainCtx(d *adminv1.Domain) context.Context {
	token := base64.StdEncoding.EncodeToString([]byte(d.Domain + ":" + d.Key))
	ctx := metadata.NewIncomingContext(context.Background(), metadata.Pairs("authorization", "Basic "+token))
	return ctx
}
