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
	trackingtypes "github.com/kannon-email/kannon/proto/kannon/tracking/types"
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

func TestTrackingPolicy(t *testing.T) {
	t.Run("A new domain starts at identified", func(t *testing.T) {
		domain := createTestDomain(t)
		assert.Equal(t, trackingtypes.TrackingMode_TRACKING_MODE_IDENTIFIED, domain.Tracking.GetOpens())
		assert.Equal(t, trackingtypes.TrackingMode_TRACKING_MODE_IDENTIFIED, domain.Tracking.GetLinks())
	})

	t.Run("A policy I set is readable back", func(t *testing.T) {
		domain := createTestDomain(t)

		res, err := testservice.SetTrackingPolicy(t.Context(), connect.NewRequest(&pb.SetTrackingPolicyReq{
			Domain: domain.Domain,
			Tracking: &trackingtypes.TrackingPolicy{
				Opens: trackingtypes.TrackingMode_TRACKING_MODE_OFF,
				Links: trackingtypes.TrackingMode_TRACKING_MODE_ANONYMOUS,
			},
		}))
		assert.Nil(t, err)
		assert.Equal(t, trackingtypes.TrackingMode_TRACKING_MODE_OFF, res.Msg.Domain.Tracking.GetOpens())
		assert.Equal(t, trackingtypes.TrackingMode_TRACKING_MODE_ANONYMOUS, res.Msg.Domain.Tracking.GetLinks())

		got, err := testservice.GetDomain(t.Context(), connect.NewRequest(&pb.GetDomainReq{
			Domain: domain.Domain,
		}))
		assert.Nil(t, err)
		assert.Equal(t, trackingtypes.TrackingMode_TRACKING_MODE_OFF, got.Msg.Domain.Tracking.GetOpens())
		assert.Equal(t, trackingtypes.TrackingMode_TRACKING_MODE_ANONYMOUS, got.Msg.Domain.Tracking.GetLinks())
	})

	t.Run("Pseudonymous is rejected as unsupported", func(t *testing.T) {
		domain := createTestDomain(t)

		_, err := testservice.SetTrackingPolicy(t.Context(), connect.NewRequest(&pb.SetTrackingPolicyReq{
			Domain: domain.Domain,
			Tracking: &trackingtypes.TrackingPolicy{
				Opens: trackingtypes.TrackingMode_TRACKING_MODE_PSEUDONYMOUS,
				Links: trackingtypes.TrackingMode_TRACKING_MODE_OFF,
			},
		}))
		assert.Equal(t, connect.CodeUnimplemented, connect.CodeOf(err))

		// The refused call must leave the domain as it was.
		got, err := testservice.GetDomain(t.Context(), connect.NewRequest(&pb.GetDomainReq{
			Domain: domain.Domain,
		}))
		assert.Nil(t, err)
		assert.Equal(t, trackingtypes.TrackingMode_TRACKING_MODE_IDENTIFIED, got.Msg.Domain.Tracking.GetOpens())
	})

	t.Run("An unknown domain is not found", func(t *testing.T) {
		_, err := testservice.SetTrackingPolicy(t.Context(), connect.NewRequest(&pb.SetTrackingPolicyReq{
			Domain: tests.FakeDomain(t),
			Tracking: &trackingtypes.TrackingPolicy{
				Opens: trackingtypes.TrackingMode_TRACKING_MODE_OFF,
				Links: trackingtypes.TrackingMode_TRACKING_MODE_OFF,
			},
		}))
		assert.Equal(t, connect.CodeNotFound, connect.CodeOf(err))
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
