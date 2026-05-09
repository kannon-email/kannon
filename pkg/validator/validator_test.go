package validator_test

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
	"github.com/kannon-email/kannon/internal/pool"
	"github.com/kannon-email/kannon/internal/runner"
	"github.com/kannon-email/kannon/internal/tests"
	"github.com/kannon-email/kannon/pkg/api/adminapi"
	"github.com/kannon-email/kannon/pkg/api/mailapi"
	"github.com/kannon-email/kannon/pkg/validator"
	adminv1connect "github.com/kannon-email/kannon/proto/kannon/admin/apiv1/apiv1connect"
	mailerapiv1 "github.com/kannon-email/kannon/proto/kannon/mailer/apiv1"
	mailerv1connect "github.com/kannon-email/kannon/proto/kannon/mailer/apiv1/apiv1connect"
	mailertypes "github.com/kannon-email/kannon/proto/kannon/mailer/types"

	"github.com/stretchr/testify/assert"
	"google.golang.org/protobuf/types/known/timestamppb"
)

var db *pgxpool.Pool
var q *sqlc.Queries
var vt *validator.Validator
var mp MockPublisher

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
	claimer := pool.NewClaimer(sqlc.NewDeliveryRepository(db, delivery.DefaultBackoff))
	vt = validator.NewValidator(claimer, &mp)

	ts = mailapi.NewMailerAPIV1(db, delivery.DefaultBackoff)
	adminAPI = adminapi.CreateAdminAPIService(db)

	code := m.Run()

	// You can't defer this because os.Exit doesn't care for defer
	if err := purge(); err != nil {
		slog.Error("Could not purge resource", "err", err)
		os.Exit(1)
	}

	os.Exit(code)
}

func TestLoop(t *testing.T) {
	err := runner.Run(t.Context(), vt.Cycle, runner.MaxLoop(1))
	assert.Nil(t, err)
}

func TestValidEmail(t *testing.T) {
	domain := createTestDomain(t)
	sendEmail(t, domain, "valid@email.com")
	sendEmail(t, domain, "valid@email2.com")

	runOneCycle(t)

	assert.Len(t, mp.subjects, 2)
	for _, subj := range mp.subjects {
		assert.Equal(t, "kannon.stats.accepted", subj)
	}

	t.Cleanup(func() {
		mp.subjects = nil
	})
}

func TestInvalidEmail(t *testing.T) {
	domain := createTestDomain(t)
	sendEmail(t, domain, "invalid-email.com")
	sendEmail(t, domain, "invalid-email2.com")

	runOneCycle(t)

	assert.Len(t, mp.subjects, 2)
	assert.Contains(t, mp.subjects, "kannon.stats.rejected")

	t.Cleanup(func() {
		mp.subjects = nil
	})
}

func runOneCycle(t *testing.T) {
	t.Helper()
	err := runner.Run(t.Context(), vt.Cycle, runner.MaxLoop(1))
	assert.Nil(t, err)
}

func sendEmail(t *testing.T, domainWithKey *tests.DomainWithKey, email string) {
	t.Helper()

	req := connect.NewRequest(&mailerapiv1.SendHTMLReq{
		Sender: &mailertypes.Sender{
			Email: "test@email.com",
			Alias: "test",
		},
		Subject:       "Ciao",
		Html:          "My htnml",
		ScheduledTime: timestamppb.Now(),
		Recipients: []*mailertypes.Recipient{
			{
				Email: email,
			},
		},
	})

	authRequest(req, domainWithKey)

	_, err := ts.SendHTML(t.Context(), req)
	assert.Nil(t, err)
}

func createTestDomain(t *testing.T) *tests.DomainWithKey {
	t.Helper()
	return tests.CreateTestDomain(t, adminAPI)
}

func authRequest[T any](req *connect.Request[T], d *tests.DomainWithKey) {
	token := base64.StdEncoding.EncodeToString([]byte(d.Domain.Domain + ":" + d.APIKey))
	req.Header().Set("Authorization", "Basic "+token)
}

type MockPublisher struct {
	subjects []string
}

func (m *MockPublisher) Publish(subj string, data []byte) error {
	m.subjects = append(m.subjects, subj)
	return nil
}
