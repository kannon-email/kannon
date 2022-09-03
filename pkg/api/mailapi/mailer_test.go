package mailapi_test

import (
	"context"
	"strings"
	"testing"

	sqlc "github.com/ludusrusso/kannon/internal/db"
	"github.com/stretchr/testify/assert"
	"google.golang.org/protobuf/types/known/timestamppb"

	mailerv1 "github.com/ludusrusso/kannon/proto/kannon/mailer/apiv1"
	types "github.com/ludusrusso/kannon/proto/kannon/mailer/types"
)

func TestInsertMail(t *testing.T) {
	d := createTestDomain(t)

	ctx := getDomainCtx(d)

	res, err := ts.SendHTML(ctx, &mailerv1.SendHTMLReq{
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
		ScheduledTime: timestamppb.Now(),
	})
	assert.Nil(t, err)
	assert.NotEmpty(t, res.MessageId)
	assert.NotEmpty(t, res.TemplateId)
	assert.True(t, strings.HasSuffix(res.MessageId, "@"+d.Domain))
	assert.True(t, strings.HasSuffix(res.TemplateId, "@"+d.Domain))

	sp, err := q.GetSendingPoolsEmails(context.Background(), sqlc.GetSendingPoolsEmailsParams{
		MessageID: res.MessageId,
		Limit:     100,
		Offset:    0,
	})
	assert.Nil(t, err)
	assert.Equal(t, 1, len(sp))
	assert.Equal(t, "test@email.com", sp[0].Email)
	assert.Equal(t, sqlc.SendingPoolStatusToVerify, sp[0].Status)
	assert.Equal(t, "Test", sp[0].Fields["name"])
	cleanDB(t)
}
