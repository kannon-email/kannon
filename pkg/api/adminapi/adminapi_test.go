package adminapi_test

import (
	"context"
	"log/slog"
	"os"
	"testing"

	"connectrpc.com/connect"
	"github.com/jackc/pgx/v5/pgxpool"
	schema "github.com/kannon-email/kannon/db"
	"github.com/kannon-email/kannon/internal/authz"
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

// adminCtx is what makes the tests in this package reach the handler at all: they hold the handler
// rather than the server, so no interceptor has run, nothing has installed a Principal and every
// operation would refuse. It hands them the authority a request with the admin token arrives with.
func adminCtx(t *testing.T) context.Context {
	t.Helper()
	return tests.AdminContext(t.Context())
}

func TestEmptyDatabase(t *testing.T) {
	res, err := testservice.GetDomains(adminCtx(t), connect.NewRequest(&pb.GetDomainsReq{}))
	assert.Nil(t, err)
	assert.Empty(t, len(res.Msg.Domains))
}

// What a request whose Principal never got installed meets, seen from outside: every operation
// refuses as permission denied, not as the internal fault this adapter used to answer for anything.
// The whole surface is walked, because the failure guarded against is one method wired past its service.
func TestEveryOperationRefusesAnUnauthenticatedRequest(t *testing.T) {
	// A Template id in the composed format, so that the three legacy adapters get past
	// their parse and are refused by the guard rather than by the identifier.
	const someTemplate = "template_unauthorized@example.com"

	calls := []struct {
		name string
		call func(ctx context.Context) error
	}{
		{"GetDomains", func(ctx context.Context) error {
			_, err := testservice.GetDomains(ctx, connect.NewRequest(&pb.GetDomainsReq{}))
			return err
		}},
		{"GetDomain", func(ctx context.Context) error {
			_, err := testservice.GetDomain(ctx, connect.NewRequest(&pb.GetDomainReq{Domain: "example.com"}))
			return err
		}},
		{"CreateDomain", func(ctx context.Context) error {
			_, err := testservice.CreateDomain(ctx, connect.NewRequest(&pb.CreateDomainRequest{Domain: "refused.example.com"}))
			return err
		}},
		{"SetTrackingPolicy", func(ctx context.Context) error {
			_, err := testservice.SetTrackingPolicy(ctx, connect.NewRequest(&pb.SetTrackingPolicyReq{
				Domain: "example.com",
				Tracking: &trackingtypes.TrackingPolicy{
					Opens: trackingtypes.TrackingMode_TRACKING_MODE_OFF,
					Links: trackingtypes.TrackingMode_TRACKING_MODE_OFF,
				},
			}))
			return err
		}},
		{"CreateTemplate", func(ctx context.Context) error {
			_, err := testservice.CreateTemplate(ctx, connect.NewRequest(&pb.CreateTemplateReq{Domain: "example.com", Html: "hi", Title: "hi"}))
			return err
		}},
		{"GetTemplates", func(ctx context.Context) error {
			_, err := testservice.GetTemplates(ctx, connect.NewRequest(&pb.GetTemplatesReq{Domain: "example.com", Take: 10}))
			return err
		}},
		{"GetTemplate", func(ctx context.Context) error {
			_, err := testservice.GetTemplate(ctx, connect.NewRequest(&pb.GetTemplateReq{TemplateId: someTemplate}))
			return err
		}},
		{"UpdateTemplate", func(ctx context.Context) error {
			_, err := testservice.UpdateTemplate(ctx, connect.NewRequest(&pb.UpdateTemplateReq{TemplateId: someTemplate, Html: "hi"}))
			return err
		}},
		{"DeleteTemplate", func(ctx context.Context) error {
			_, err := testservice.DeleteTemplate(ctx, connect.NewRequest(&pb.DeleteTemplateReq{TemplateId: someTemplate}))
			return err
		}},
		{"CreateAPIKey", func(ctx context.Context) error {
			_, err := testservice.CreateAPIKey(ctx, connect.NewRequest(&pb.CreateAPIKeyRequest{Domain: "example.com", Name: "refused"}))
			return err
		}},
		{"ListAPIKeys", func(ctx context.Context) error {
			_, err := testservice.ListAPIKeys(ctx, connect.NewRequest(&pb.ListAPIKeysRequest{Domain: "example.com"}))
			return err
		}},
		{"GetAPIKey", func(ctx context.Context) error {
			_, err := testservice.GetAPIKey(ctx, connect.NewRequest(&pb.GetAPIKeyRequest{Domain: "example.com", Id: "key_refused"}))
			return err
		}},
		{"DeactivateAPIKey", func(ctx context.Context) error {
			_, err := testservice.DeactivateAPIKey(ctx, connect.NewRequest(&pb.DeactivateAPIKeyRequest{Domain: "example.com", Id: "key_refused"}))
			return err
		}},
	}

	for _, tc := range calls {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.call(t.Context())

			assert.Equal(t, connect.CodePermissionDenied, connect.CodeOf(err))
			assert.ErrorIs(t, err, authz.ErrNoPrincipal)
		})
	}
}

func TestCreateANewDomain(t *testing.T) {
	newDomain := tests.FakeDomain(t)

	var domain *pb.Domain

	t.Run("When I create a domain", func(t *testing.T) {
		var err error
		res, err := testservice.CreateDomain(adminCtx(t), connect.NewRequest(&pb.CreateDomainRequest{
			Domain: newDomain,
		}))
		domain = res.Msg
		assert.Nil(t, err)
		assert.Equal(t, newDomain, domain.Domain)
		assert.NotEmpty(t, domain.DkimPubKey)
	})

	t.Run("I Should find 1 domain in the datastore", func(t *testing.T) {
		resGetDomains, err := testservice.GetDomains(adminCtx(t), connect.NewRequest(&pb.GetDomainsReq{}))
		assert.Nil(t, err)
		assert.Equal(t, 1, len(resGetDomains.Msg.Domains))
	})

	t.Run("I Should query the created domain", func(t *testing.T) {
		resGetDomain, err := testservice.GetDomain(adminCtx(t), connect.NewRequest(&pb.GetDomainReq{
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

	// Every rung of the scale is a Policy an operator may set, Pseudonymous
	// included: it was refused while it was reserved, and since #424 (ADR 0006)
	// it is stored and read back like any other Mode.
	t.Run("A policy I set is readable back", func(t *testing.T) {
		cases := []struct {
			name  string
			opens trackingtypes.TrackingMode
			links trackingtypes.TrackingMode
		}{
			{
				name:  "Off and anonymous",
				opens: trackingtypes.TrackingMode_TRACKING_MODE_OFF,
				links: trackingtypes.TrackingMode_TRACKING_MODE_ANONYMOUS,
			},
			{
				name:  "Pseudonymous and off",
				opens: trackingtypes.TrackingMode_TRACKING_MODE_PSEUDONYMOUS,
				links: trackingtypes.TrackingMode_TRACKING_MODE_OFF,
			},
		}

		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				domain := createTestDomain(t)

				res, err := testservice.SetTrackingPolicy(adminCtx(t), connect.NewRequest(&pb.SetTrackingPolicyReq{
					Domain: domain.Domain,
					Tracking: &trackingtypes.TrackingPolicy{
						Opens: tc.opens,
						Links: tc.links,
					},
				}))
				assert.Nil(t, err)
				assert.Equal(t, tc.opens, res.Msg.Domain.Tracking.GetOpens())
				assert.Equal(t, tc.links, res.Msg.Domain.Tracking.GetLinks())

				got, err := testservice.GetDomain(adminCtx(t), connect.NewRequest(&pb.GetDomainReq{
					Domain: domain.Domain,
				}))
				assert.Nil(t, err)
				assert.Equal(t, tc.opens, got.Msg.Domain.Tracking.GetOpens())
				assert.Equal(t, tc.links, got.Msg.Domain.Tracking.GetLinks())
			})
		}
	})

	// A wire value this build cannot read comes from a client built against a
	// newer schema. It is the one Mode the Admin API still refuses, because
	// guessing what it meant would set a ceiling nobody asked for.
	t.Run("A Mode this build does not know is an invalid argument", func(t *testing.T) {
		domain := createTestDomain(t)

		_, err := testservice.SetTrackingPolicy(adminCtx(t), connect.NewRequest(&pb.SetTrackingPolicyReq{
			Domain: domain.Domain,
			Tracking: &trackingtypes.TrackingPolicy{
				Opens: trackingtypes.TrackingMode(9999),
				Links: trackingtypes.TrackingMode_TRACKING_MODE_OFF,
			},
		}))
		assert.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err))

		// The refused call must leave the domain as it was.
		got, err := testservice.GetDomain(adminCtx(t), connect.NewRequest(&pb.GetDomainReq{
			Domain: domain.Domain,
		}))
		assert.Nil(t, err)
		assert.Equal(t, trackingtypes.TrackingMode_TRACKING_MODE_IDENTIFIED, got.Msg.Domain.Tracking.GetOpens())
	})

	t.Run("An unknown domain is not found", func(t *testing.T) {
		_, err := testservice.SetTrackingPolicy(adminCtx(t), connect.NewRequest(&pb.SetTrackingPolicyReq{
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
	res, err := testservice.CreateDomain(adminCtx(t), connect.NewRequest(&pb.CreateDomainRequest{
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
