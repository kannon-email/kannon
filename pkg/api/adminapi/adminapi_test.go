package adminapi_test

import (
	"context"
	"os"
	"testing"

	"connectrpc.com/connect"
	"github.com/jackc/pgx/v5/pgxpool"
	schema "github.com/kannon-email/kannon/db"
	sqlc "github.com/kannon-email/kannon/internal/db"
	"github.com/kannon-email/kannon/internal/tests"
	"github.com/kannon-email/kannon/pkg/api/adminapi"
	pb "github.com/kannon-email/kannon/proto/kannon/admin/apiv1"
	adminv1connect "github.com/kannon-email/kannon/proto/kannon/admin/apiv1/apiv1connect"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"

	_ "github.com/lib/pq"
)

var db *pgxpool.Pool
var q *sqlc.Queries
var testservice adminv1connect.ApiHandler

func TestMain(m *testing.M) {
	var purge tests.PurgeFunc
	var err error

	db, purge, err = tests.TestPostgresInit(schema.Schema)
	if err != nil {
		logrus.Fatalf("Could not start resource: %s", err)
	}

	q = sqlc.New(db)
	testservice = adminapi.CreateAdminAPIService(q, db)

	code := m.Run()

	// You can't defer this because os.Exit doesn't care for defer
	if err := purge(); err != nil {
		logrus.Fatalf("Could not purge resource: %s", err)
	}

	os.Exit(code)
}

func TestEmptyDatabase(t *testing.T) {
	res, err := testservice.GetDomains(context.Background(), connect.NewRequest(&pb.GetDomainsReq{}))
	assert.Nil(t, err)
	assert.Empty(t, len(res.Msg.Domains))
}

func TestCreateANewDomain(t *testing.T) {
	newDomain := "test.test.test"

	var domain *pb.Domain

	// When I create a domain
	t.Run("When I create a domain", func(t *testing.T) {
		var err error
		res, err := testservice.CreateDomain(context.Background(), connect.NewRequest(&pb.CreateDomainRequest{
			Domain: newDomain,
		}))
		domain = res.Msg
		assert.Nil(t, err)
		assert.Equal(t, newDomain, domain.Domain)
		assert.NotEmpty(t, domain.DkimPubKey)
	})

	t.Run("I Should find 1 domain in the datastore", func(t *testing.T) {
		resGetDomains, err := testservice.GetDomains(context.Background(), connect.NewRequest(&pb.GetDomainsReq{}))
		assert.Nil(t, err)
		assert.Equal(t, 1, len(resGetDomains.Msg.Domains))
	})

	t.Run("I Should query the created domain", func(t *testing.T) {
		resGetDomain, err := testservice.GetDomain(context.Background(), connect.NewRequest(&pb.GetDomainReq{
			Domain: newDomain,
		}))
		assert.Nil(t, err)
		assert.Equal(t, newDomain, resGetDomain.Msg.Domain.Domain)
	})

	cleanDB(t)
}

func createTestDomain(t *testing.T) *pb.Domain {
	res, err := testservice.CreateDomain(context.Background(), connect.NewRequest(&pb.CreateDomainRequest{
		Domain: "test.test.test",
	}))
	assert.Nil(t, err)
	return res.Msg
}

func cleanDB(t *testing.T) {
	_, err := db.Exec(context.Background(), "DELETE FROM domains")
	assert.Nil(t, err)

	_, err = db.Exec(context.Background(), "DELETE FROM sending_pool_emails")
	assert.Nil(t, err)

	_, err = db.Exec(context.Background(), "DELETE FROM templates")
	assert.Nil(t, err)
}
