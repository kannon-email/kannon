package adminapi_test

import (
	"log/slog"
	"os"
	"testing"

	"connectrpc.com/connect"
	"github.com/jackc/pgx/v5/pgxpool"
	schema "github.com/kannon-email/kannon/db"
	"github.com/kannon-email/kannon/internal/tests"
	"github.com/kannon-email/kannon/pkg/api/adminapi"
	pb "github.com/kannon-email/kannon/proto/kannon/admin/apiv1"
	adminv1connect "github.com/kannon-email/kannon/proto/kannon/admin/apiv1/apiv1connect"
	"github.com/stretchr/testify/assert"
)

var db *pgxpool.Pool
var testservice adminv1connect.ApiHandler

func TestMain(m *testing.M) {
	var purge tests.PurgeFunc
	var err error

	db, purge, err = tests.TestPostgresInit(schema.Schema)
	if err != nil {
		slog.Error("Could not start resource", "err", err)
		os.Exit(1)
	}

	testservice = adminapi.CreateAdminAPIService(db)

	code := m.Run()

	// You can't defer this because os.Exit doesn't care for defer
	if err := purge(); err != nil {
		slog.Error("Could not purge resource", "err", err)
		os.Exit(1)
	}

	os.Exit(code)
}

func TestEmptyDatabase(t *testing.T) {
	res, err := testservice.GetDomains(t.Context(), connect.NewRequest(&pb.GetDomainsReq{}))
	assert.Nil(t, err)
	assert.Empty(t, len(res.Msg.Domains))
}

func TestCreateANewDomain(t *testing.T) {
	newDomain := tests.FakeDomain(t)

	var domain *pb.Domain

	// When I create a domain
	t.Run("When I create a domain", func(t *testing.T) {
		var err error
		res, err := testservice.CreateDomain(t.Context(), connect.NewRequest(&pb.CreateDomainRequest{
			Domain: newDomain,
		}))
		domain = res.Msg
		assert.Nil(t, err)
		assert.Equal(t, newDomain, domain.Domain)
		assert.NotEmpty(t, domain.DkimPubKey)
	})

	t.Run("I Should find 1 domain in the datastore", func(t *testing.T) {
		resGetDomains, err := testservice.GetDomains(t.Context(), connect.NewRequest(&pb.GetDomainsReq{}))
		assert.Nil(t, err)
		assert.Equal(t, 1, len(resGetDomains.Msg.Domains))
	})

	t.Run("I Should query the created domain", func(t *testing.T) {
		resGetDomain, err := testservice.GetDomain(t.Context(), connect.NewRequest(&pb.GetDomainReq{
			Domain: newDomain,
		}))
		assert.Nil(t, err)
		assert.Equal(t, newDomain, resGetDomain.Msg.Domain.Domain)
	})

	cleanDB(t)
}

func createTestDomain(t *testing.T) *pb.Domain {
	domain := tests.FakeDomain(t)
	res, err := testservice.CreateDomain(t.Context(), connect.NewRequest(&pb.CreateDomainRequest{
		Domain: domain,
	}))
	assert.Nil(t, err)
	return res.Msg
}

func cleanDB(t *testing.T) {
	_, err := db.Exec(t.Context(), "DELETE FROM domains")
	assert.Nil(t, err)

	_, err = db.Exec(t.Context(), "DELETE FROM sending_pool_emails")
	assert.Nil(t, err)

	_, err = db.Exec(t.Context(), "DELETE FROM templates")
	assert.Nil(t, err)
}
