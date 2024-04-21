package adminapi

import (
	"context"

	"connectrpc.com/connect"
	pb "github.com/ludusrusso/kannon/proto/kannon/admin/apiv1"
)

func (s *adminAPIService) CreateTemplate(ctx context.Context, req *connect.Request[pb.CreateTemplateReq]) (*connect.Response[pb.CreateTemplateRes], error) {
	res, err := s.tm.CreateTemplate(ctx, req.Msg.Html, req.Msg.Domain, req.Msg.Title)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&pb.CreateTemplateRes{
		Template: res.Pb(),
	}), nil
}

func (s *adminAPIService) UpdateTemplate(ctx context.Context, req *connect.Request[pb.UpdateTemplateReq]) (*connect.Response[pb.UpdateTemplateRes], error) {
	res, err := s.tm.UpdateTemplate(ctx, req.Msg.TemplateId, req.Msg.Html, req.Msg.Title)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&pb.UpdateTemplateRes{
		Template: res.Pb(),
	}), nil
}

func (s *adminAPIService) DeleteTemplate(ctx context.Context, req *connect.Request[pb.DeleteTemplateReq]) (*connect.Response[pb.DeleteTemplateRes], error) {
	res, err := s.tm.DeleteTemplate(ctx, req.Msg.TemplateId)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&pb.DeleteTemplateRes{
		Template: res.Pb(),
	}), nil
}

func (s *adminAPIService) GetTemplate(ctx context.Context, req *connect.Request[pb.GetTemplateReq]) (*connect.Response[pb.GetTemplateRes], error) {
	res, err := s.tm.GetTemplate(ctx, req.Msg.TemplateId)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&pb.GetTemplateRes{
		Template: res.Pb(),
	}), nil
}

func (s *adminAPIService) GetTemplates(ctx context.Context, req *connect.Request[pb.GetTemplatesReq]) (*connect.Response[pb.GetTemplatesRes], error) {
	res, total, err := s.tm.GetTemplates(ctx, req.Msg.Domain, uint(req.Msg.Skip), uint(req.Msg.Take))
	if err != nil {
		return nil, err
	}

	pbTemplates := make([]*pb.Template, 0, len(res))
	for _, t := range res {
		pbTemplates = append(pbTemplates, t.Pb())
	}

	return connect.NewResponse(&pb.GetTemplatesRes{
		Templates: pbTemplates,
		Total:     uint32(total),
	}), nil
}
