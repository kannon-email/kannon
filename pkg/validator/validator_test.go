package validator_test

import (
	"context"
	"encoding/base64"
	"os"
	"testing"

	"connectrpc.com/connect"
	"github.com/jackc/pgx/v5/pgxpool"
	schema "github.com/kannon-email/kannon/db"
	sqlc "github.com/kannon-email/kannon/internal/db"
	"github.com/kannon-email/kannon/internal/pool"
	"github.com/kannon-email/kannon/internal/runner"
	"github.com/kannon-email/kannon/internal/tests"
	"github.com/kannon-email/kannon/pkg/api/adminapi"
	"github.com/kannon-email/kannon/pkg/api/mailapi"
	"github.com/kannon-email/kannon/pkg/validator"
	adminapiv1 "github.com/kannon-email/kannon/proto/kannon/admin/apiv1"
	adminv1connect "github.com/kannon-email/kannon/proto/kannon/admin/apiv1/apiv1connect"
	mailerapiv1 "github.com/kannon-email/kannon/proto/kannon/mailer/apiv1"
	mailerv1connect "github.com/kannon-email/kannon/proto/kannon/mailer/apiv1/apiv1connect"
	mailertypes "github.com/kannon-email/kannon/proto/kannon/mailer/types"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"google.golang.org/protobuf/types/known/timestamppb"

	_ "github.com/lib/pq"
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
		logrus.Fatalf("Could not start resource: %s", err)
	}

	q = sqlc.New(db)
	pm := pool.NewSendingPoolManager(q)
	vt = validator.NewValidator(pm, &mp)

	ts = mailapi.NewMailerAPIV1(q, db)
	adminAPI = adminapi.CreateAdminAPIService(q, db)

	code := m.Run()

	// You can't defer this because os.Exit doesn't care for defer
	if err := purge(); err != nil {
		logrus.Fatalf("Could not purge resource: %s", err)
	}

	os.Exit(code)
}

func TestLoop(t *testing.T) {
	err := runner.Run(context.Background(), vt.Cycle, runner.MaxLoop(1))
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
		cleanDB(t)
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
		cleanDB(t)
	})
}

func runOneCycle(t *testing.T) {
	t.Helper()
	err := runner.Run(context.Background(), vt.Cycle, runner.MaxLoop(1))
	assert.Nil(t, err)
}

type testDomainWithKey struct {
	domain *adminapiv1.Domain
	apiKey string
}

func sendEmail(t *testing.T, domainWithKey *testDomainWithKey, email string) {
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

	_, err := ts.SendHTML(context.Background(), req)
	assert.Nil(t, err)
}

func createTestDomain(t *testing.T) *testDomainWithKey {
	t.Helper()
	res, err := adminAPI.CreateDomain(context.Background(), connect.NewRequest(&adminapiv1.CreateDomainRequest{
		Domain: "test.test.test",
	}))
	assert.Nil(t, err)

	// Create an API key for authentication
	keyRes, err := adminAPI.CreateAPIKey(context.Background(), connect.NewRequest(&adminapiv1.CreateAPIKeyRequest{
		Domain: res.Msg.Domain,
		Name:   "test-key",
	}))
	assert.Nil(t, err)

	return &testDomainWithKey{
		domain: res.Msg,
		apiKey: keyRes.Msg.ApiKey.Key,
	}
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

func authRequest[T any](req *connect.Request[T], d *testDomainWithKey) {
	token := base64.StdEncoding.EncodeToString([]byte(d.domain.Domain + ":" + d.apiKey))
	req.Header().Set("Authorization", "Basic "+token)
}

type MockPublisher struct {
	subjects []string
}

func (m *MockPublisher) Publish(subj string, data []byte) error {
	m.subjects = append(m.subjects, subj)
	return nil
}
