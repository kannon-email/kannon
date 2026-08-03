package adminapi_test

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"connectrpc.com/connect"
	pb "github.com/kannon-email/kannon/proto/kannon/admin/apiv1"
	"github.com/stretchr/testify/assert"
)

func TestCreateTemplate(t *testing.T) {
	d := createTestDomain(t)
	ctx := adminCtx(t)

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
	ctx := adminCtx(t)

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
	ctx := adminCtx(t)

	t1 := createTemplate(t, ctx, d, "Hello {{ name }}")

	res, err := testservice.DeleteTemplate(ctx, connect.NewRequest(&pb.DeleteTemplateReq{
		TemplateId: t1.TemplateId,
	}))
	assert.Nil(t, err)
	assert.Equal(t, t1.TemplateId, res.Msg.Template.TemplateId)

	// The Domain has to be stated: a list scoped to no Domain used to come back
	// empty, which made this assertion pass for the wrong reason. GetTemplates now
	// refuses a request that names no Domain, so the test says which one it means.
	resG, err := testservice.GetTemplates(ctx, connect.NewRequest(&pb.GetTemplatesReq{
		Skip:   0,
		Take:   10,
		Domain: d.Domain,
	}))
	assert.Nil(t, err)
	assert.Equal(t, uint32(0), resG.Msg.Total)

	cleanDB(t)
}

func TestGetTemplates(t *testing.T) {
	d := createTestDomain(t)
	ctx := adminCtx(t)

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
	ctx := adminCtx(t)

	t1 := createTemplate(t, ctx, d, "Hello {{ name }}")

	res, err := testservice.UpdateTemplate(ctx, connect.NewRequest(&pb.UpdateTemplateReq{
		TemplateId: t1.TemplateId,
		Html:       "Hello Updated",
	}))

	assert.Nil(t, err)
	assert.Equal(t, t1.TemplateId, res.Msg.Template.TemplateId)
	assert.Equal(t, "Hello Updated", res.Msg.Template.Html)

	cleanDB(t)
}

// The three operations whose request carries no Domain recover it from the identifier, so an
// identifier carrying none cannot be served at all — there is nothing to authorize against. Each of
// the three is walked, because each is a separate adapter and the recovery is three separate lines.
func TestDomainlessOperationsRefuseAnIdThatCarriesNoDomain(t *testing.T) {
	ctx := adminCtx(t)

	ids := []struct {
		name string
		id   string
	}{
		{"no separator", "template_ckv0d2n"},
		{"two separators", "template_ckv0d2n@a.com@b.com"},
		{"single-label domain", "template_ckv0d2n@templates"},
		{"empty", ""},
	}

	for _, tc := range ids {
		t.Run(tc.name, func(t *testing.T) {
			_, err := testservice.GetTemplate(ctx, connect.NewRequest(&pb.GetTemplateReq{TemplateId: tc.id}))
			assert.Error(t, err)

			_, err = testservice.UpdateTemplate(ctx, connect.NewRequest(&pb.UpdateTemplateReq{TemplateId: tc.id, Html: "x"}))
			assert.Error(t, err)

			_, err = testservice.DeleteTemplate(ctx, connect.NewRequest(&pb.DeleteTemplateReq{TemplateId: tc.id}))
			assert.Error(t, err)
		})
	}
}

// And the recovered Domain is the Domain the load is scoped to: a Template of one Domain is not
// reachable by asking for it under another. The id is rewritten to name a second Domain — all a
// caller could do — and the guard then authorizes against that Domain while the lookup finds nothing.
func TestDomainlessOperationsCannotReachAnotherDomainsTemplate(t *testing.T) {
	first := createTestDomain(t)
	second := createTestDomain(t)
	ctx := adminCtx(t)

	tpl := createTemplate(t, ctx, first, "Hello {{ name }}")
	forged := strings.Replace(tpl.TemplateId, "@"+first.Domain, "@"+second.Domain, 1)

	_, err := testservice.GetTemplate(ctx, connect.NewRequest(&pb.GetTemplateReq{TemplateId: forged}))
	assert.Error(t, err)

	// The original is untouched and still readable under its own Domain.
	got, err := testservice.GetTemplate(ctx, connect.NewRequest(&pb.GetTemplateReq{TemplateId: tpl.TemplateId}))
	assert.Nil(t, err)
	assert.Equal(t, "Hello {{ name }}", got.Msg.Template.Html)

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
