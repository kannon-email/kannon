package validator_test

import (
	"encoding/base64"
	"log/slog"
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
	tests.Main(func() int {
		var purge tests.PurgeFunc
		var err error

		db, purge, err = tests.TestPostgresInit(schema.Schema)
		if err != nil {
			slog.Error("Could not start resource", "err", err)
			return 1
		}

		q = sqlc.New(db)
		claimer := pool.NewClaimer(sqlc.NewDeliveryRepository(db, delivery.DefaultBackoff, delivery.DefaultRetryWindow))
		vt = validator.NewValidator(claimer, &mp, nil)

		ts = mailapi.NewMailerAPIV1(db, delivery.DefaultBackoff, delivery.DefaultRetryWindow, 0, nil)
		adminAPI = adminapi.CreateAdminAPIService(db)

		code := m.Run()

		if err := purge(); err != nil {
			slog.Error("Could not purge resource", "err", err)
			return 1
		}

		return code
	})
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
			Email: "test@" + domainWithKey.Domain.Domain,
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

// The Validator is the last check before the SMTPSender dials anyone, so it refuses an address
// that names a host rather than a mail domain even when the Delivery predates intake refusing it,
// and matches the whole address rather than an address somewhere inside it.
func TestValidatorRejectsAddressesItCannotDeliverTo(t *testing.T) {
	domain := createTestDomain(t)
	refused := []string{
		"x@[127.0.0.1]",
		"x@10.0.0.5",
		"x@[IPv6:::1]",
		"x@::1",
		"x@localhost",
		"x@foo.localhost",
		"junk\r\nvictim@ok.com trailing",
		"prefix victim@ok.com",
	}
	for _, email := range refused {
		sendEmail(t, domain, "placeholder@email.com")
		_, err := db.Exec(t.Context(), "UPDATE sending_pool_emails SET email = $1 WHERE email = 'placeholder@email.com'", email)
		assert.Nil(t, err)
	}
	sendEmail(t, domain, "Valid.Name+tag@Example.com")
	t.Cleanup(func() { mp.subjects = nil })

	runOneCycle(t)

	rejected := 0
	for _, subj := range mp.subjects {
		if subj == "kannon.stats.rejected" {
			rejected++
		}
	}
	assert.Len(t, mp.subjects, len(refused)+1)
	assert.Equal(t, len(refused), rejected)
	assert.Contains(t, mp.subjects, "kannon.stats.accepted")
}
