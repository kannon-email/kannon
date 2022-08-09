package mailapi

import (
	"context"

	"github.com/ludusrusso/kannon/generated/pb"
)

func (s *mailAPIService) CreateTemplate(ctx context.Context, req *pb.CreateTemplateReq) (*pb.CreateTemplateRes, error) {
	res, err := s.templates.CreateTemplate(ctx, req.Html, req.Domain, req.Title)
	if err != nil {
		return nil, err
	}
	return &pb.CreateTemplateRes{
		Template: res.Pb(),
	}, nil
}

func (s *mailAPIService) UpdateTemplate(ctx context.Context, req *pb.UpdateTemplateReq) (*pb.UpdateTemplateRes, error) {
	res, err := s.templates.UpdateTemplate(ctx, req.TemplateId, req.Html, req.Title)
	if err != nil {
		return nil, err
	}
	return &pb.UpdateTemplateRes{
		Template: res.Pb(),
	}, nil
}

func (s *mailAPIService) DeleteTemplate(ctx context.Context, req *pb.DeleteTemplateReq) (*pb.DeleteTemplateRes, error) {
	res, err := s.templates.DeleteTemplate(ctx, req.TemplateId)
	if err != nil {
		return nil, err
	}
	return &pb.DeleteTemplateRes{
		Template: res.Pb(),
	}, nil
}

func (s *mailAPIService) GetTemplate(ctx context.Context, req *pb.GetTemplateReq) (*pb.GetTemplateRes, error) {
	res, err := s.templates.GetTemplate(ctx, req.TemplateId)
	if err != nil {
		return nil, err
	}
	return &pb.GetTemplateRes{
		Template: res.Pb(),
	}, nil
}

func (s *mailAPIService) GetTemplates(ctx context.Context, req *pb.GetTemplatesReq) (*pb.GetTemplatesRes, error) {
	res, total, err := s.templates.GetTemplates(ctx, req.Domain, uint(req.Skip), uint(req.Take))
	if err != nil {
		return nil, err
	}

	pbTemplates := make([]*pb.Template, 0, len(res))
	for _, t := range res {
		pbTemplates = append(pbTemplates, t.Pb())
	}

	return &pb.GetTemplatesRes{
		Templates: pbTemplates,
		Total:     uint32(total),
	}, nil
}
