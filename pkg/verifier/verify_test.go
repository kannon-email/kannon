package verifier_test

import (
	"context"
	"database/sql"
	"encoding/base64"
	"os"
	"testing"

	schema "github.com/ludusrusso/kannon/db"
	sqlc "github.com/ludusrusso/kannon/internal/db"
	"github.com/ludusrusso/kannon/internal/pool"
	"github.com/ludusrusso/kannon/internal/runner"
	"github.com/ludusrusso/kannon/internal/tests"
	"github.com/ludusrusso/kannon/mocks"
	"github.com/ludusrusso/kannon/pkg/api/adminapi"
	"github.com/ludusrusso/kannon/pkg/api/mailapi"
	"github.com/ludusrusso/kannon/pkg/verifier"
	adminapiv1 "github.com/ludusrusso/kannon/proto/kannon/admin/apiv1"
	mailerapiv1 "github.com/ludusrusso/kannon/proto/kannon/mailer/apiv1"
	mailertypes "github.com/ludusrusso/kannon/proto/kannon/mailer/types"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"google.golang.org/grpc/metadata"
	"google.golang.org/protobuf/types/known/timestamppb"

	_ "github.com/lib/pq"
)

var db *sql.DB
var q *sqlc.Queries
var vt *verifier.Verifier
var mp mocks.Publisher

var ts mailerapiv1.MailerServer
var adminAPI adminapiv1.ApiServer

func TestMain(m *testing.M) {
	var purge tests.PurgeFunc
	var err error

	db, purge, err = tests.TestPostgresInit(schema.Schema)
	if err != nil {
		logrus.Fatalf("Could not start resource: %s", err)
	}

	q = sqlc.New(db)
	pm := pool.NewSendingPoolManager(q)
	vt = verifier.NewVerifier(pm, &mp)

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

	mp.EXPECT().Publish("kannon.stats.accepted", mock.Anything).Return(nil)

	runOneCycle(t)

	mp.AssertNumberOfCalls(t, "Publish", 2)

	t.Cleanup(func() {
		mp.ExpectedCalls = nil
		mp.Calls = nil
		cleanDB(t)
	})
}

func TestInvalidEmail(t *testing.T) {
	domain := createTestDomain(t)
	sendEmail(t, domain, "invalid-email.com")
	sendEmail(t, domain, "invalid-email2.com")

	mp.EXPECT().Publish("kannon.stats.rejected", mock.Anything).Return(nil)
	runOneCycle(t)

	mp.AssertNumberOfCalls(t, "Publish", 2)

	t.Cleanup(func() {
		mp.ExpectedCalls = nil
		mp.Calls = nil
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
	_, err := ts.SendHTML(ctx, &mailerapiv1.SendHTMLReq{
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
	assert.Nil(t, err)
}

func createTestDomain(t *testing.T) *adminapiv1.Domain {
	t.Helper()
	res, err := adminAPI.CreateDomain(context.Background(), &adminapiv1.CreateDomainRequest{
		Domain: "test.test.test",
	})
	assert.Nil(t, err)
	return res
}

func cleanDB(t *testing.T) {
	t.Helper()
	_, err := db.ExecContext(context.Background(), "DELETE FROM domains")
	assert.Nil(t, err)

	_, err = db.ExecContext(context.Background(), "DELETE FROM sending_pool_emails")
	assert.Nil(t, err)

	_, err = db.ExecContext(context.Background(), "DELETE FROM templates")
	assert.Nil(t, err)
}

func getDomainCtx(d *adminapiv1.Domain) context.Context {
	token := base64.StdEncoding.EncodeToString([]byte(d.Domain + ":" + d.Key))
	ctx := metadata.NewIncomingContext(context.Background(), metadata.Pairs("authorization", "Basic "+token))
	return ctx
}
