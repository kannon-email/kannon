package adminapi

import (
	"context"

	sqlc "github.com/kannon-email/kannon/internal/db"
	"github.com/kannon-email/kannon/internal/domains"
	"github.com/kannon-email/kannon/internal/templates"

	"connectrpc.com/connect"
	pb "github.com/kannon-email/kannon/proto/kannon/admin/apiv1"
	adminv1connect "github.com/kannon-email/kannon/proto/kannon/admin/apiv1/apiv1connect"
)

// Adapter to Connect handler interface

type adminAPIConnectAdapter struct {
	impl *adminAPIService
}

func (a *adminAPIConnectAdapter) GetDomains(ctx context.Context, req *connect.Request[pb.GetDomainsReq]) (*connect.Response[pb.GetDomainsResponse], error) {
	resp, err := a.impl.GetDomains(ctx, req.Msg)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	return connect.NewResponse(resp), nil
}

func (a *adminAPIConnectAdapter) GetDomain(ctx context.Context, req *connect.Request[pb.GetDomainReq]) (*connect.Response[pb.GetDomainRes], error) {
	resp, err := a.impl.GetDomain(ctx, req.Msg)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	return connect.NewResponse(resp), nil
}

func (a *adminAPIConnectAdapter) CreateDomain(ctx context.Context, req *connect.Request[pb.CreateDomainRequest]) (*connect.Response[pb.Domain], error) {
	resp, err := a.impl.CreateDomain(ctx, req.Msg)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	return connect.NewResponse(resp), nil
}

func (a *adminAPIConnectAdapter) RegenerateDomainKey(ctx context.Context, req *connect.Request[pb.RegenerateDomainKeyRequest]) (*connect.Response[pb.Domain], error) {
	resp, err := a.impl.RegenerateDomainKey(ctx, req.Msg)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	return connect.NewResponse(resp), nil
}

func (a *adminAPIConnectAdapter) CreateTemplate(ctx context.Context, req *connect.Request[pb.CreateTemplateReq]) (*connect.Response[pb.CreateTemplateRes], error) {
	resp, err := a.impl.CreateTemplate(ctx, req.Msg)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	return connect.NewResponse(resp), nil
}

func (a *adminAPIConnectAdapter) UpdateTemplate(ctx context.Context, req *connect.Request[pb.UpdateTemplateReq]) (*connect.Response[pb.UpdateTemplateRes], error) {
	resp, err := a.impl.UpdateTemplate(ctx, req.Msg)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	return connect.NewResponse(resp), nil
}

func (a *adminAPIConnectAdapter) DeleteTemplate(ctx context.Context, req *connect.Request[pb.DeleteTemplateReq]) (*connect.Response[pb.DeleteTemplateRes], error) {
	resp, err := a.impl.DeleteTemplate(ctx, req.Msg)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	return connect.NewResponse(resp), nil
}

func (a *adminAPIConnectAdapter) GetTemplate(ctx context.Context, req *connect.Request[pb.GetTemplateReq]) (*connect.Response[pb.GetTemplateRes], error) {
	resp, err := a.impl.GetTemplate(ctx, req.Msg)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	return connect.NewResponse(resp), nil
}

func (a *adminAPIConnectAdapter) GetTemplates(ctx context.Context, req *connect.Request[pb.GetTemplatesReq]) (*connect.Response[pb.GetTemplatesRes], error) {
	resp, err := a.impl.GetTemplates(ctx, req.Msg)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	return connect.NewResponse(resp), nil
}

func CreateAdminAPIService(q *sqlc.Queries) adminv1connect.ApiHandler {
	dm := domains.NewDomainManager(q)
	tm := templates.NewTemplateManager(q)
	return &adminAPIConnectAdapter{
		impl: &adminAPIService{
			dm: dm,
			tm: tm,
		},
	}
}
