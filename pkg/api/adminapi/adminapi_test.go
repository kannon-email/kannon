package adminapi_test

import (
	"context"
	"database/sql"
	"os"
	"testing"

	schema "github.com/ludusrusso/kannon/db"
	sqlc "github.com/ludusrusso/kannon/internal/db"
	"github.com/ludusrusso/kannon/internal/tests"
	"github.com/ludusrusso/kannon/pkg/api/adminapi"
	pb "github.com/ludusrusso/kannon/proto/kannon/admin/apiv1"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"

	_ "github.com/lib/pq"
)

var db *sql.DB
var q *sqlc.Queries
var testservice pb.ApiServer

func TestMain(m *testing.M) {
	var purge tests.PurgeFunc
	var err error

	db, purge, err = tests.TestPostgresInit(schema.Schema)
	if err != nil {
		logrus.Fatalf("Could not start resource: %s", err)
	}

	q = sqlc.New(db)
	testservice = adminapi.CreateAdminAPIService(q)

	code := m.Run()

	// You can't defer this because os.Exit doesn't care for defer
	if err := purge(); err != nil {
		logrus.Fatalf("Could not purge resource: %s", err)
	}

	os.Exit(code)
}

func TestEmptyDatabase(t *testing.T) {
	res, err := testservice.GetDomains(context.Background(), &pb.GetDomainsReq{})
	assert.Nil(t, err)
	assert.Empty(t, len(res.Domains))
}

func TestCreateANewDomain(t *testing.T) {
	newDomain := "test.test.test"

	var domain *pb.Domain

	// When I create a domain
	t.Run("When I create a domain", func(t *testing.T) {
		var err error
		domain, err = testservice.CreateDomain(context.Background(), &pb.CreateDomainRequest{
			Domain: newDomain,
		})
		assert.Nil(t, err)
		assert.Equal(t, newDomain, domain.Domain)
		assert.NotEmpty(t, domain.Key)
		assert.NotEmpty(t, domain.DkimPubKey)
	})

	t.Run("I Should find 1 domain in the datastore", func(t *testing.T) {
		resGetDomains, err := testservice.GetDomains(context.Background(), &pb.GetDomainsReq{})
		assert.Nil(t, err)
		assert.Equal(t, 1, len(resGetDomains.Domains))
	})

	t.Run("I should be able to change the key", func(t *testing.T) {
		domain2, err := testservice.RegenerateDomainKey(context.Background(), &pb.RegenerateDomainKeyRequest{
			Domain: newDomain,
		})
		assert.Nil(t, err)
		assert.NotEqual(t, domain.Key, domain2.Key)
	})

	cleanDB(t)
}

func createTestDomain(t *testing.T) *pb.Domain {
	res, err := testservice.CreateDomain(context.Background(), &pb.CreateDomainRequest{
		Domain: "test.test.test",
	})
	assert.Nil(t, err)
	return res
}

func cleanDB(t *testing.T) {
	_, err := db.ExecContext(context.Background(), "DELETE FROM domains")
	assert.Nil(t, err)

	_, err = db.ExecContext(context.Background(), "DELETE FROM sending_pool_emails")
	assert.Nil(t, err)

	_, err = db.ExecContext(context.Background(), "DELETE FROM templates")
	assert.Nil(t, err)
}
