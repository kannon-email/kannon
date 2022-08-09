package mailapi_test

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/ludusrusso/kannon/generated/pb"
	"github.com/stretchr/testify/assert"
)

func TestCreateTemplate(t *testing.T) {
	d := createTestDomain(t)
	ctx := getDomainCtx(d)

	res, err := ts.CreateTemplate(ctx, &pb.CreateTemplateReq{
		Html:   "Hello {{ name }}",
		Title:  "Hello",
		Domain: d.Domain,
	})
	assert.Nil(t, err)
	assert.True(t, strings.HasSuffix(res.Template.TemplateId, "@"+d.Domain), fmt.Errorf("template id should have domain suffix: %v, %v", res.Template.TemplateId, d.Domain))
	cleanDB(t)
}

func TestGetTemplate(t *testing.T) {
	d := createTestDomain(t)
	ctx := getDomainCtx(d)

	t1 := createTemplate(t, ctx, d, "Hello {{ name }}")

	res, err := ts.GetTemplate(ctx, &pb.GetTemplateReq{
		TemplateId: t1.TemplateId,
	})
	assert.Nil(t, err)
	assert.Equal(t, t1.TemplateId, res.Template.TemplateId)
	cleanDB(t)
}

func TestDeleteTemplate(t *testing.T) {
	d := createTestDomain(t)
	ctx := getDomainCtx(d)

	t1 := createTemplate(t, ctx, d, "Hello {{ name }}")

	res, err := ts.DeleteTemplate(ctx, &pb.DeleteTemplateReq{
		TemplateId: t1.TemplateId,
	})
	assert.Nil(t, err)
	assert.Equal(t, t1.TemplateId, res.Template.TemplateId)

	resG, err := ts.GetTemplates(ctx, &pb.GetTemplatesReq{
		Skip: 0,
		Take: 10,
	})
	assert.Nil(t, err)
	assert.Equal(t, uint32(0), resG.Total)

	cleanDB(t)
}

func TestGetTemplates(t *testing.T) {
	d := createTestDomain(t)
	ctx := getDomainCtx(d)

	t1 := createTemplate(t, ctx, d, "Hello {{ name }}")
	t2 := createTemplate(t, ctx, d, "Hello 2 {{ name }}")

	res, err := ts.GetTemplates(ctx, &pb.GetTemplatesReq{
		Skip:   0,
		Take:   10,
		Domain: d.Domain,
	})
	assert.Nil(t, err)
	assert.Equal(t, uint32(2), res.Total)

	assert.Equal(t, t1.TemplateId, res.Templates[0].TemplateId)
	assert.Equal(t, t2.TemplateId, res.Templates[1].TemplateId)

	cleanDB(t)
}

func TestUpdateTemplates(t *testing.T) {
	d := createTestDomain(t)
	ctx := getDomainCtx(d)

	t1 := createTemplate(t, ctx, d, "Hello {{ name }}")

	// update template
	res, err := ts.UpdateTemplate(ctx, &pb.UpdateTemplateReq{
		TemplateId: t1.TemplateId,
		Html:       "Hello Updated",
	})

	assert.Nil(t, err)
	assert.Equal(t, t1.TemplateId, res.Template.TemplateId)
	assert.Equal(t, "Hello Updated", res.Template.Html)

	cleanDB(t)
}

func createTemplate(t *testing.T, ctx context.Context, d *pb.Domain, html string) *pb.Template {
	res, err := ts.CreateTemplate(ctx, &pb.CreateTemplateReq{
		Html:   html,
		Title:  "Hello",
		Domain: d.Domain,
	})
	assert.Nil(t, err)
	return res.Template
}
