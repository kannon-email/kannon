package adminapi

import (
	"context"

	"github.com/kannon-email/kannon/internal/templates"
	pb "github.com/kannon-email/kannon/proto/kannon/admin/apiv1"
)

func (s *adminAPIService) CreateTemplate(ctx context.Context, req *pb.CreateTemplateReq) (*pb.CreateTemplateRes, error) {
	tpl, err := templates.NewPersistent(req.Domain, req.Html, req.Title)
	if err != nil {
		return nil, err
	}
	if err := s.templates.Create(ctx, tpl); err != nil {
		return nil, err
	}
	return &pb.CreateTemplateRes{Template: tpl.Pb()}, nil
}

func (s *adminAPIService) UpdateTemplate(ctx context.Context, req *pb.UpdateTemplateReq) (*pb.UpdateTemplateRes, error) {
	updated, err := s.templates.Update(ctx, req.TemplateId, func(t *templates.Template) error {
		t.SetHTML(req.Html)
		t.SetTitle(req.Title)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &pb.UpdateTemplateRes{Template: updated.Pb()}, nil
}

func (s *adminAPIService) DeleteTemplate(ctx context.Context, req *pb.DeleteTemplateReq) (*pb.DeleteTemplateRes, error) {
	deleted, err := s.templates.Delete(ctx, req.TemplateId)
	if err != nil {
		return nil, err
	}
	return &pb.DeleteTemplateRes{Template: deleted.Pb()}, nil
}

func (s *adminAPIService) GetTemplate(ctx context.Context, req *pb.GetTemplateReq) (*pb.GetTemplateRes, error) {
	tpl, err := s.templates.GetByID(ctx, req.TemplateId)
	if err != nil {
		return nil, err
	}
	return &pb.GetTemplateRes{Template: tpl.Pb()}, nil
}

func (s *adminAPIService) GetTemplates(ctx context.Context, req *pb.GetTemplatesReq) (*pb.GetTemplatesRes, error) {
	tpls, err := s.templates.List(ctx, req.Domain, templates.Pagination{Skip: uint(req.Skip), Take: uint(req.Take)})
	if err != nil {
		return nil, err
	}
	total, err := s.templates.Count(ctx, req.Domain)
	if err != nil {
		return nil, err
	}

	pbTemplates := make([]*pb.Template, 0, len(tpls))
	for _, t := range tpls {
		pbTemplates = append(pbTemplates, t.Pb())
	}

	return &pb.GetTemplatesRes{
		Templates: pbTemplates,
		Total:     uint32(total),
	}, nil
}
