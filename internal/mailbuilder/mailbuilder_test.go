package mailbuilder_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"io"
	"net/mail"
	"os"
	"strings"
	"testing"

	"connectrpc.com/connect"
	"github.com/jackc/pgx/v5/pgxpool"
	schema "github.com/ludusrusso/kannon/db"
	sqlc "github.com/ludusrusso/kannon/internal/db"
	"github.com/ludusrusso/kannon/internal/mailbuilder"
	"github.com/ludusrusso/kannon/internal/pool"
	"github.com/ludusrusso/kannon/internal/statssec"
	"github.com/ludusrusso/kannon/internal/tests"
	"github.com/ludusrusso/kannon/pkg/api/adminapi"
	"github.com/ludusrusso/kannon/pkg/api/mailapi"
	pb "github.com/ludusrusso/kannon/proto/kannon/mailer/types"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"google.golang.org/grpc/metadata"
	"google.golang.org/protobuf/types/known/timestamppb"

	adminapiv1 "github.com/ludusrusso/kannon/proto/kannon/admin/apiv1"
	adminv1connect "github.com/ludusrusso/kannon/proto/kannon/admin/apiv1/apiv1connect"
	mailerapiv1 "github.com/ludusrusso/kannon/proto/kannon/mailer/apiv1"
	mailerv1connect "github.com/ludusrusso/kannon/proto/kannon/mailer/apiv1/apiv1connect"

	_ "github.com/lib/pq"
)

var db *pgxpool.Pool
var q *sqlc.Queries
var mb mailbuilder.MailBulder
var ma mailerv1connect.MailerHandler
var adminAPI adminv1connect.ApiHandler
var pm pool.SendingPoolManager

func TestMain(m *testing.M) {
	var purge tests.PurgeFunc
	var err error

	db, purge, err = tests.TestPostgresInit(schema.Schema)
	if err != nil {
		logrus.Fatalf("Could not start resource: %s", err)
	}

	q = sqlc.New(db)

	mb = mailbuilder.NewMailBuilder(q, statssec.NewStatsService(q))
	ma = mailapi.NewMailerAPIV1(q)
	adminAPI = adminapi.CreateAdminAPIService(q)
	pm = pool.NewSendingPoolManager(q)

	code := m.Run()

	// You can't defer this because os.Exit doesn't care for defer
	if err := purge(); err != nil {
		logrus.Fatalf("Could not purge resource: %s", err)
	}

	os.Exit(code)
}

func TestPrepareMail(t *testing.T) {
	d, err := adminAPI.CreateDomain(context.Background(), connect.NewRequest(&adminapiv1.CreateDomainRequest{
		Domain: "test.com",
	}))
	assert.Nil(t, err)

	token := base64.StdEncoding.EncodeToString([]byte(d.Msg.Domain + ":" + d.Msg.Key))
	ctx := metadata.NewIncomingContext(context.Background(), metadata.Pairs("authorization", "Basic "+token))

	res, err := ma.SendHTML(ctx, connect.NewRequest(&mailerapiv1.SendHTMLReq{
		Sender: &pb.Sender{
			Email: "test@test.com",
			Alias: "Test",
		},
		Subject:       "Test {{ name }}",
		Html:          "test {{name }}",
		ScheduledTime: timestamppb.Now(),
		Recipients: []*pb.Recipient{
			{
				Email: "test@emailtest.com",
				Fields: map[string]string{
					"name": "Test",
				},
			},
		},
	}))
	assert.Nil(t, err)

	err = pm.SetScheduled(ctx, res.Msg.MessageId, "test@emailtest.com")
	assert.Nil(t, err)

	emails, err := pm.PrepareForSend(context.Background(), 1)
	assert.Nil(t, err)
	assert.Equal(t, 1, len(emails))

	m, err := mb.BuildEmail(context.Background(), emails[0])
	assert.Nil(t, err)
	parsed, err := mail.ReadMessage(bytes.NewReader(m.Body))
	assert.Nil(t, err)

	assert.Nil(t, err)
	assert.Equal(t, "test@emailtest.com", parsed.Header.Get("To"))
	assert.Equal(t, "Test <test@test.com>", parsed.Header.Get("From"))

	// test subject
	assert.Equal(t, "Test Test", parsed.Header.Get("Subject"))

	// test html
	html, _ := io.ReadAll(parsed.Body)

	assert.Equal(t, "test Test", string(html))
}

func TestPrepareMailWithAttachments(t *testing.T) {
	d, err := adminAPI.CreateDomain(context.Background(), connect.NewRequest(&adminapiv1.CreateDomainRequest{
		Domain: "test2.com",
	}))
	assert.Nil(t, err)

	token := base64.StdEncoding.EncodeToString([]byte(d.Msg.Domain + ":" + d.Msg.Key))
	ctx := metadata.NewIncomingContext(context.Background(), metadata.Pairs("authorization", "Basic "+token))

	res, err := ma.SendHTML(ctx, connect.NewRequest(&mailerapiv1.SendHTMLReq{
		Sender: &pb.Sender{
			Email: "test@test.com",
			Alias: "Test",
		},
		Subject:       "Test {{ name }}",
		Html:          "test {{name }}",
		ScheduledTime: timestamppb.Now(),
		Recipients: []*pb.Recipient{
			{
				Email: "test@emailtest.com",
				Fields: map[string]string{
					"name": "Test",
				},
			},
		},
		Attachments: []*mailerapiv1.Attachment{
			{
				Filename: "test.txt",
				Content:  []byte("test"),
			},
		},
	}))
	assert.Nil(t, err)

	err = pm.SetScheduled(ctx, res.Msg.MessageId, "test@emailtest.com")
	assert.Nil(t, err)

	emails, err := pm.PrepareForSend(context.Background(), 1)
	assert.Nil(t, err)
	assert.Equal(t, 1, len(emails))

	m, err := mb.BuildEmail(context.Background(), emails[0])
	assert.Nil(t, err)
	parsed, err := mail.ReadMessage(bytes.NewReader(m.Body))
	assert.Nil(t, err)

	assert.Nil(t, err)
	assert.Equal(t, "test@emailtest.com", parsed.Header.Get("To"))
	assert.Equal(t, "Test <test@test.com>", parsed.Header.Get("From"))
	assert.Equal(t, "multipart/mixed", strings.Split(parsed.Header.Get("Content-Type"), ";")[0])
}
