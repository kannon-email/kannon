package mailapi_test

import (
	"context"
	"testing"

	"github.com/ludusrusso/kannon/generated/pb"
	"github.com/stretchr/testify/assert"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestInsertMail(t *testing.T) {
	d := createTestDomain(t)

	ctx := getDomainCtx(d)

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

	assert.Nil(t, err)
	assert.Equal(t, "Test", sp[0].Fields["name"])
	cleanDB(t)
}
