package validator_test

import (
	"context"
	"encoding/base64"
	"os"
	"testing"

	"connectrpc.com/connect"
	"github.com/jackc/pgx/v5/pgxpool"
	schema "github.com/ludusrusso/kannon/db"
	sqlc "github.com/ludusrusso/kannon/internal/db"
	"github.com/ludusrusso/kannon/internal/pool"
	"github.com/ludusrusso/kannon/internal/runner"
	"github.com/ludusrusso/kannon/internal/tests"
	"github.com/ludusrusso/kannon/pkg/api/adminapi"
	"github.com/ludusrusso/kannon/pkg/api/mailapi"
	"github.com/ludusrusso/kannon/pkg/validator"
	adminapiv1 "github.com/ludusrusso/kannon/proto/kannon/admin/apiv1"
	adminv1connect "github.com/ludusrusso/kannon/proto/kannon/admin/apiv1/apiv1connect"
	mailerapiv1 "github.com/ludusrusso/kannon/proto/kannon/mailer/apiv1"
	mailerv1connect "github.com/ludusrusso/kannon/proto/kannon/mailer/apiv1/apiv1connect"
	mailertypes "github.com/ludusrusso/kannon/proto/kannon/mailer/types"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"google.golang.org/grpc/metadata"
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
	vt = validator.NewValidator(pm, &mp, nil)

	ts = mailapi.NewMailerAPIV1(q)
	adminAPI = adminapi.CreateAdminAPIService(q)

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

func sendEmail(t *testing.T, domain *adminapiv1.Domain, email string) {
	t.Helper()

	ctx := getDomainCtx(domain)
	_, err := ts.SendHTML(ctx, connect.NewRequest(&mailerapiv1.SendHTMLReq{
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
	}))
	assert.Nil(t, err)
}

func createTestDomain(t *testing.T) *adminapiv1.Domain {
	t.Helper()
	res, err := adminAPI.CreateDomain(context.Background(), connect.NewRequest(&adminapiv1.CreateDomainRequest{
		Domain: "test.test.test",
	}))
	assert.Nil(t, err)
	return res.Msg
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

func getDomainCtx(d *adminapiv1.Domain) context.Context {
	token := base64.StdEncoding.EncodeToString([]byte(d.Domain + ":" + d.Key))
	ctx := metadata.NewIncomingContext(context.Background(), metadata.Pairs("authorization", "Basic "+token))
	return ctx
}

type MockPublisher struct {
	subjects []string
}

func (m *MockPublisher) Publish(subj string, data []byte) error {
	m.subjects = append(m.subjects, subj)
	return nil
}
