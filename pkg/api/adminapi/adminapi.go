package adminapi

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/kannon-email/kannon/internal/apikeys"
	sqlc "github.com/kannon-email/kannon/internal/db"
	"github.com/kannon-email/kannon/internal/domains"
	"github.com/kannon-email/kannon/internal/templates"

	"connectrpc.com/connect"
	"github.com/kannon-email/kannon/pkg/api/connecterr"
	pb "github.com/kannon-email/kannon/proto/kannon/admin/apiv1"
	adminv1connect "github.com/kannon-email/kannon/proto/kannon/admin/apiv1/apiv1connect"
)

type adminAPIConnectAdapter struct {
	impl *adminAPIService
}

func (a *adminAPIConnectAdapter) GetDomains(ctx context.Context, req *connect.Request[pb.GetDomainsReq]) (*connect.Response[pb.GetDomainsResponse], error) {
	resp, err := a.impl.GetDomains(ctx, req.Msg)
	if err != nil {
		return nil, connecterr.Error(err)
	}
	return connect.NewResponse(resp), nil
}

func (a *adminAPIConnectAdapter) GetDomain(ctx context.Context, req *connect.Request[pb.GetDomainReq]) (*connect.Response[pb.GetDomainRes], error) {
	resp, err := a.impl.GetDomain(ctx, req.Msg)
	if err != nil {
		return nil, connecterr.Error(err)
	}
	return connect.NewResponse(resp), nil
}

func (a *adminAPIConnectAdapter) CreateDomain(ctx context.Context, req *connect.Request[pb.CreateDomainRequest]) (*connect.Response[pb.Domain], error) {
	resp, err := a.impl.CreateDomain(ctx, req.Msg)
	if err != nil {
		return nil, connecterr.Error(err)
	}
	return connect.NewResponse(resp), nil
}

func (a *adminAPIConnectAdapter) SetTrackingPolicy(ctx context.Context, req *connect.Request[pb.SetTrackingPolicyReq]) (*connect.Response[pb.SetTrackingPolicyRes], error) {
	resp, err := a.impl.SetTrackingPolicy(ctx, req.Msg)
	if err != nil {
		return nil, connecterr.Error(err)
	}
	return connect.NewResponse(resp), nil
}

func (a *adminAPIConnectAdapter) CreateTemplate(ctx context.Context, req *connect.Request[pb.CreateTemplateReq]) (*connect.Response[pb.CreateTemplateRes], error) {
	resp, err := a.impl.CreateTemplate(ctx, req.Msg)
	if err != nil {
		return nil, connecterr.Error(err)
	}
	return connect.NewResponse(resp), nil
}

func (a *adminAPIConnectAdapter) UpdateTemplate(ctx context.Context, req *connect.Request[pb.UpdateTemplateReq]) (*connect.Response[pb.UpdateTemplateRes], error) {
	resp, err := a.impl.UpdateTemplate(ctx, req.Msg)
	if err != nil {
		return nil, connecterr.Error(err)
	}
	return connect.NewResponse(resp), nil
}

func (a *adminAPIConnectAdapter) DeleteTemplate(ctx context.Context, req *connect.Request[pb.DeleteTemplateReq]) (*connect.Response[pb.DeleteTemplateRes], error) {
	resp, err := a.impl.DeleteTemplate(ctx, req.Msg)
	if err != nil {
		return nil, connecterr.Error(err)
	}
	return connect.NewResponse(resp), nil
}

func (a *adminAPIConnectAdapter) GetTemplate(ctx context.Context, req *connect.Request[pb.GetTemplateReq]) (*connect.Response[pb.GetTemplateRes], error) {
	resp, err := a.impl.GetTemplate(ctx, req.Msg)
	if err != nil {
		return nil, connecterr.Error(err)
	}
	return connect.NewResponse(resp), nil
}

func (a *adminAPIConnectAdapter) GetTemplates(ctx context.Context, req *connect.Request[pb.GetTemplatesReq]) (*connect.Response[pb.GetTemplatesRes], error) {
	resp, err := a.impl.GetTemplates(ctx, req.Msg)
	if err != nil {
		return nil, connecterr.Error(err)
	}
	return connect.NewResponse(resp), nil
}

func (a *adminAPIConnectAdapter) CreateAPIKey(ctx context.Context, req *connect.Request[pb.CreateAPIKeyRequest]) (*connect.Response[pb.CreateAPIKeyResponse], error) {
	resp, err := a.impl.CreateAPIKey(ctx, req.Msg)
	if err != nil {
		return nil, connecterr.Error(err)
	}
	return connect.NewResponse(resp), nil
}

func (a *adminAPIConnectAdapter) ListAPIKeys(ctx context.Context, req *connect.Request[pb.ListAPIKeysRequest]) (*connect.Response[pb.ListAPIKeysResponse], error) {
	resp, err := a.impl.ListAPIKeys(ctx, req.Msg)
	if err != nil {
		return nil, connecterr.Error(err)
	}
	return connect.NewResponse(resp), nil
}

func (a *adminAPIConnectAdapter) GetAPIKey(ctx context.Context, req *connect.Request[pb.GetAPIKeyRequest]) (*connect.Response[pb.GetAPIKeyResponse], error) {
	resp, err := a.impl.GetAPIKey(ctx, req.Msg)
	if err != nil {
		return nil, connecterr.Error(err)
	}
	return connect.NewResponse(resp), nil
}

func (a *adminAPIConnectAdapter) DeactivateAPIKey(ctx context.Context, req *connect.Request[pb.DeactivateAPIKeyRequest]) (*connect.Response[pb.DeactivateAPIKeyResponse], error) {
	resp, err := a.impl.DeactivateAPIKey(ctx, req.Msg)
	if err != nil {
		return nil, connecterr.Error(err)
	}
	return connect.NewResponse(resp), nil
}

// CreateAdminAPIService assembles the Admin API over the three guarded services. Note what it
// does not do: it installs no Principal, so a caller holding this handler reaches operations that
// refuse unless something put one in the context — in production, the interceptor in pkg/api.
func CreateAdminAPIService(db *pgxpool.Pool) adminv1connect.ApiHandler {
	domainsRepo := sqlc.NewDomainsRepository(db)
	templatesRepo := sqlc.NewTemplatesRepository(db)
	apiKeysRepo := sqlc.NewAPIKeysRepository(db)
	return &adminAPIConnectAdapter{
		impl: &adminAPIService{
			domains:   domains.NewService(domainsRepo),
			templates: templates.NewService(templatesRepo),
			apiKeys:   apikeys.NewService(apiKeysRepo),
		},
	}
}
