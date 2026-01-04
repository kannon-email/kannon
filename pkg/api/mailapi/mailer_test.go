package mailapi_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect"
	sqlc "github.com/kannon-email/kannon/internal/db"
	"github.com/stretchr/testify/assert"
	"google.golang.org/protobuf/types/known/timestamppb"

	mailerv1 "github.com/kannon-email/kannon/proto/kannon/mailer/apiv1"
	types "github.com/kannon-email/kannon/proto/kannon/mailer/types"
)

func TestInsertMail(t *testing.T) {
	defer cleanDB(t)

	d := createTestDomain(t)

	schedTime := time.Now().Add(10 * time.Minute).Truncate(1 * time.Second)
	req := connect.NewRequest(&mailerv1.SendHTMLReq{
		Sender: &types.Sender{
			Email: "test@test.com",
			Alias: "Test",
		},
		Recipients: []*types.Recipient{
			{
				Email: "test@email.com",
				Fields: map[string]string{
					"name": "Test",
				},
			},
		},
		Subject:       "Test",
		Html:          "Hello {{ name }}",
		ScheduledTime: timestamppb.New(schedTime),
	})

	authRequest(req, d)

	res, err := ts.SendHTML(context.Background(), req)

	assert.Nil(t, err)
	assert.NotEmpty(t, res.Msg.MessageId)
	assert.NotEmpty(t, res.Msg.TemplateId)
	assert.True(t, strings.HasSuffix(res.Msg.MessageId, "@"+d.Domain.Domain))
	assert.True(t, strings.HasSuffix(res.Msg.TemplateId, "@"+d.Domain.Domain))

	sp, err := q.GetSendingPoolsEmails(context.Background(), sqlc.GetSendingPoolsEmailsParams{
		MessageID: res.Msg.MessageId,
		Limit:     100,
		Offset:    0,
	})
	assert.Nil(t, err)
	assert.Equal(t, 1, len(sp))
	assert.Equal(t, "test@email.com", sp[0].Email)
	assert.Equal(t, sqlc.SendingPoolStatusToValidate, sp[0].Status)
	assert.Equal(t, "Test", sp[0].Fields["name"])

	assert.Equal(t, schedTime.UTC(), sp[0].ScheduledTime.Time.UTC())
}

func TestSendMailWithGlobalFields(t *testing.T) {
	defer cleanDB(t)

	d := createTestDomain(t)

	schedTime := time.Now().Add(10 * time.Minute).Truncate(1 * time.Second)

	req := connect.NewRequest(&mailerv1.SendHTMLReq{
		Sender: &types.Sender{
			Email: "test@test.com",
			Alias: "Test",
		},
		Subject:       "Test",
		Html:          "Hello {{ name }}",
		ScheduledTime: timestamppb.New(schedTime),
		GlobalFields: map[string]string{
			"name": "Global",
		},
	})
	authRequest(req, d)

	res, err := ts.SendHTML(context.Background(), req)

	assert.Nil(t, err)
	assert.NotEmpty(t, res.Msg.MessageId)
	assert.NotEmpty(t, res.Msg.TemplateId)

	template, err := q.GetTemplate(context.Background(), res.Msg.TemplateId)
	assert.NoError(t, err)
	assert.Equal(t, "Hello Global", template.Html)
}

func TestSendTemplateWithGlobalFields(t *testing.T) {
	defer cleanDB(t)

	d := createTestDomain(t)

	schedTime := time.Now().Add(10 * time.Minute).Truncate(1 * time.Second)

	tmp, err := q.CreateTemplate(context.Background(), sqlc.CreateTemplateParams{
		Html:       "Hello {{ name }}",
		TemplateID: "test-template",
		Title:      "Test",
		Domain:     d.Domain.Domain,
		Type:       sqlc.TemplateTypeTemplate,
	})
	assert.NoError(t, err)

	req := connect.NewRequest(&mailerv1.SendTemplateReq{
		Sender: &types.Sender{
			Email: "test@test.com",
			Alias: "Test",
		},
		Subject:       "Test",
		TemplateId:    tmp.TemplateID,
		ScheduledTime: timestamppb.New(schedTime),
		GlobalFields: map[string]string{
			"name": "Global",
		},
	})
	authRequest(req, d)

	res, err := ts.SendTemplate(context.Background(), req)

	assert.Nil(t, err)
	assert.NotEmpty(t, res.Msg.MessageId)
	assert.NotEmpty(t, res.Msg.TemplateId)

	template, err := q.GetTemplate(context.Background(), res.Msg.TemplateId)
	assert.NoError(t, err)
	assert.NotEqual(t, tmp.ID, template.ID)
	assert.Equal(t, "Hello Global", template.Html)
}
