package mailapi_test

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"os"
	"testing"

	schema "github.com/ludusrusso/kannon/db"
	"github.com/ludusrusso/kannon/generated/pb"
	sqlc "github.com/ludusrusso/kannon/internal/db"
	"github.com/ludusrusso/kannon/internal/tests"
	"github.com/ludusrusso/kannon/pkg/api/adminapi"
	"github.com/ludusrusso/kannon/pkg/api/mailapi"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"google.golang.org/grpc/metadata"
	"google.golang.org/protobuf/types/known/timestamppb"
)

var db *sql.DB
var q *sqlc.Queries
var ts pb.MailerServer
var adminAPI pb.ApiServer

func TestMain(m *testing.M) {
	var purge tests.PurgeFunc
	var err error

	db, purge, err = tests.TestPostgresInit(schema.Schema)
	if err != nil {
		logrus.Fatalf("Could not start resource: %s", err)
	}

	q = sqlc.New(db)
	ts = mailapi.NewMailAPIService(q)
	adminAPI = adminapi.CreateAdminAPIService(q)

	code := m.Run()

	// You can't defer this because os.Exit doesn't care for defer
	if err := purge(); err != nil {
		logrus.Fatalf("Could not purge resource: %s", err)
	}

	os.Exit(code)
}

func TestInsertMail(t *testing.T) {
	d := createTestDomain(t)

	token := base64.StdEncoding.EncodeToString([]byte(d.Domain + ":" + d.Key))
	ctx := metadata.NewIncomingContext(context.Background(), metadata.Pairs("authorization", "Basic "+token))

	_, err := ts.SendHTML(ctx, &pb.SendHTMLReq{
		Sender: &pb.Sender{
			Email: "test@test.com",
			Alias: "Test",
		},
		Recipients: []*pb.Recipient{
			{
				Email: "test@email.com",
				Fields: map[string]string{
					"name": "Test",
				},
			},
		},
		Subject:       "Test",
		Html:          "Hello {{ name }}",
		ScheduledTime: timestamppb.Now(),
	})
	assert.Nil(t, err)

	sp, err := q.PrepareForSend(context.Background(), 1)
	assert.Nil(t, err)
	assert.Equal(t, 1, len(sp))
	assert.Equal(t, "test@email.com", sp[0].Email)

	var fiedls = map[string]string{}
	err = json.Unmarshal(sp[0].Fields, &fiedls)
	assert.Nil(t, err)
	assert.Equal(t, "Test", fiedls["name"])
	cleanDB(t)
}

func TestInsertMailOld(t *testing.T) {
	d := createTestDomain(t)

	token := base64.StdEncoding.EncodeToString([]byte(d.Domain + ":" + d.Key))
	ctx := metadata.NewIncomingContext(context.Background(), metadata.Pairs("authorization", "Basic "+token))

	_, err := ts.SendHTML(ctx, &pb.SendHTMLReq{
		Sender: &pb.Sender{
			Email: "test@test.com",
			Alias: "Test",
		},
		To:            []string{"test@email.com"},
		Subject:       "Test",
		Html:          "Hello {{ name }}",
		ScheduledTime: timestamppb.Now(),
	})
	assert.Nil(t, err)

	sp, err := q.PrepareForSend(context.Background(), 1)
	assert.Nil(t, err)
	assert.Equal(t, 1, len(sp))
	assert.Equal(t, "test@email.com", sp[0].Email)

	var fiedls = map[string]string{}
	err = json.Unmarshal(sp[0].Fields, &fiedls)
	assert.Nil(t, err)
	assert.Equal(t, 0, len(fiedls))
	cleanDB(t)
}

func createTestDomain(t *testing.T) *pb.Domain {
	res, err := adminAPI.CreateDomain(context.Background(), &pb.CreateDomainRequest{
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
}
