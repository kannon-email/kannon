package adminapi

import (
	"context"

	"github.com/kannon-email/kannon/internal/templates"
	"github.com/kannon-email/kannon/internal/values"
	pb "github.com/kannon-email/kannon/proto/kannon/admin/apiv1"
)

func (s *adminAPIService) CreateTemplate(ctx context.Context, req *pb.CreateTemplateReq) (*pb.CreateTemplateRes, error) {
	domain, err := values.Parse(req.Domain)
	if err != nil {
		return nil, err
	}

	tpl, err := s.templates.CreateTemplate(ctx, domain, req.Html, req.Title)
	if err != nil {
		return nil, err
	}
	return &pb.CreateTemplateRes{Template: tpl.Pb()}, nil
}

// UpdateTemplate is a legacy adapter: UpdateTemplateReq carries only a template_id, so the Domain
// is recovered from the identifier with templates.DomainFromID, which records why authorizing on a
// parsed value can only narrow. Deletable when a proto revision carries the Domain.
func (s *adminAPIService) UpdateTemplate(ctx context.Context, req *pb.UpdateTemplateReq) (*pb.UpdateTemplateRes, error) {
	domain, err := templates.DomainFromID(req.TemplateId)
	if err != nil {
		return nil, err
	}

	updated, err := s.templates.UpdateTemplate(ctx, domain, req.TemplateId, req.Html, req.Title)
	if err != nil {
		return nil, err
	}
	return &pb.UpdateTemplateRes{Template: updated.Pb()}, nil
}

// DeleteTemplate is a legacy adapter, for the reason given on UpdateTemplate:
// DeleteTemplateReq carries only a template_id, and the Domain is recovered from
// it. Deletable when the proto carries the Domain.
func (s *adminAPIService) DeleteTemplate(ctx context.Context, req *pb.DeleteTemplateReq) (*pb.DeleteTemplateRes, error) {
	domain, err := templates.DomainFromID(req.TemplateId)
	if err != nil {
		return nil, err
	}

	deleted, err := s.templates.DeleteTemplate(ctx, domain, req.TemplateId)
	if err != nil {
		return nil, err
	}
	return &pb.DeleteTemplateRes{Template: deleted.Pb()}, nil
}

// GetTemplate is a legacy adapter, for the reason given on UpdateTemplate:
// GetTemplateReq carries only a template_id, and the Domain is recovered from it.
// Deletable when the proto carries the Domain.
func (s *adminAPIService) GetTemplate(ctx context.Context, req *pb.GetTemplateReq) (*pb.GetTemplateRes, error) {
	domain, err := templates.DomainFromID(req.TemplateId)
	if err != nil {
		return nil, err
	}

	tpl, err := s.templates.GetTemplate(ctx, domain, req.TemplateId)
	if err != nil {
		return nil, err
	}
	return &pb.GetTemplateRes{Template: tpl.Pb()}, nil
}

func (s *adminAPIService) GetTemplates(ctx context.Context, req *pb.GetTemplatesReq) (*pb.GetTemplatesRes, error) {
	domain, err := values.Parse(req.Domain)
	if err != nil {
		return nil, err
	}

	tpls, total, err := s.templates.GetTemplates(ctx, domain, templates.Pagination{Skip: uint(req.Skip), Take: uint(req.Take)})
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
