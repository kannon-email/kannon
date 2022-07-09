package adminapi_test

import (
	"context"
	"database/sql"
	"os"
	"testing"

	"github.com/ludusrusso/kannon/cmd/api/adminapi"
	"github.com/ludusrusso/kannon/generated/pb"
	sqlc "github.com/ludusrusso/kannon/internal/db"
	"github.com/ludusrusso/kannon/internal/tests"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
)

var db *sql.DB
var q *sqlc.Queries
var testservice pb.ApiServer

func TestMain(m *testing.M) {
	var purge tests.PurgeFunc
	var err error

	db, purge, err = tests.TestPostgresInit()
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
}
