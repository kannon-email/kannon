package adminapi_test

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"connectrpc.com/connect"
	pb "github.com/kannon-email/kannon/proto/kannon/admin/apiv1"
	"github.com/stretchr/testify/assert"

	_ "github.com/lib/pq"
)

func TestCreateTemplate(t *testing.T) {
	d := createTestDomain(t)
	ctx := context.Background()

	res, err := testservice.CreateTemplate(ctx, connect.NewRequest(&pb.CreateTemplateReq{
		Html:   "Hello {{ name }}",
		Title:  "Hello",
		Domain: d.Domain,
	}))
	assert.Nil(t, err)
	assert.True(t, strings.HasSuffix(res.Msg.Template.TemplateId, "@"+d.Domain), fmt.Errorf("template id should have domain suffix: %v, %v", res.Msg.Template.TemplateId, d.Domain))
	cleanDB(t)
}

func TestGetTemplate(t *testing.T) {
	d := createTestDomain(t)
	ctx := context.Background()

	t1 := createTemplate(t, ctx, d, "Hello {{ name }}")

	res, err := testservice.GetTemplate(ctx, connect.NewRequest(&pb.GetTemplateReq{
		TemplateId: t1.TemplateId,
	}))
	assert.Nil(t, err)
	assert.Equal(t, t1.TemplateId, res.Msg.Template.TemplateId)
	cleanDB(t)
}

func TestDeleteTemplate(t *testing.T) {
	d := createTestDomain(t)
	ctx := context.Background()

	t1 := createTemplate(t, ctx, d, "Hello {{ name }}")

	res, err := testservice.DeleteTemplate(ctx, connect.NewRequest(&pb.DeleteTemplateReq{
		TemplateId: t1.TemplateId,
	}))
	assert.Nil(t, err)
	assert.Equal(t, t1.TemplateId, res.Msg.Template.TemplateId)

	resG, err := testservice.GetTemplates(ctx, connect.NewRequest(&pb.GetTemplatesReq{
		Skip: 0,
		Take: 10,
	}))
	assert.Nil(t, err)
	assert.Equal(t, uint32(0), resG.Msg.Total)

	cleanDB(t)
}

func TestGetTemplates(t *testing.T) {
	d := createTestDomain(t)
	ctx := context.Background()

	t1 := createTemplate(t, ctx, d, "Hello {{ name }}")
	t2 := createTemplate(t, ctx, d, "Hello 2 {{ name }}")

	res, err := testservice.GetTemplates(ctx, connect.NewRequest(&pb.GetTemplatesReq{
		Skip:   0,
		Take:   10,
		Domain: d.Domain,
	}))
	assert.Nil(t, err)
	assert.Equal(t, uint32(2), res.Msg.Total)

	assert.Equal(t, t1.TemplateId, res.Msg.Templates[0].TemplateId)
	assert.Equal(t, t2.TemplateId, res.Msg.Templates[1].TemplateId)

	cleanDB(t)
}

func TestUpdateTemplates(t *testing.T) {
	d := createTestDomain(t)
	ctx := context.Background()

	t1 := createTemplate(t, ctx, d, "Hello {{ name }}")

	// update template
	res, err := testservice.UpdateTemplate(ctx, connect.NewRequest(&pb.UpdateTemplateReq{
		TemplateId: t1.TemplateId,
		Html:       "Hello Updated",
	}))

	assert.Nil(t, err)
	assert.Equal(t, t1.TemplateId, res.Msg.Template.TemplateId)
	assert.Equal(t, "Hello Updated", res.Msg.Template.Html)

	cleanDB(t)
}

func createTemplate(t *testing.T, ctx context.Context, d *pb.Domain, html string) *pb.Template {
	res, err := testservice.CreateTemplate(ctx, connect.NewRequest(&pb.CreateTemplateReq{
		Html:   html,
		Title:  "Hello",
		Domain: d.Domain,
	}))
	assert.Nil(t, err)
	return res.Msg.Template
}
