package adminapi

import (
	"context"

	"connectrpc.com/connect"
	sqlc "github.com/ludusrusso/kannon/internal/db"
	"github.com/ludusrusso/kannon/internal/domains"
	"github.com/ludusrusso/kannon/internal/templates"

	pb "github.com/ludusrusso/kannon/proto/kannon/admin/apiv1"
)

type adminAPIService struct {
	dm domains.DomainManager
	tm templates.Manager
}

func (s *adminAPIService) GetDomains(ctx context.Context, in *connect.Request[pb.GetDomainsReq]) (*connect.Response[pb.GetDomainsResponse], error) {
	domains, err := s.dm.GetAllDomains(ctx)
	if err != nil {
		return nil, err
	}

	res := &pb.GetDomainsResponse{}
	for _, domain := range domains {
		res.Domains = append(res.Domains, dbDomainToProtoDomain(domain))
	}
	return connect.NewResponse(res), nil
}

func (s *adminAPIService) GetDomain(ctx context.Context, in *connect.Request[pb.GetDomainReq]) (*connect.Response[pb.GetDomainRes], error) {
	domain, err := s.dm.FindDomain(ctx, in.Msg.Domain)
	if err != nil {
		return nil, err
	}

	return connect.NewResponse(&pb.GetDomainRes{
		Domain: dbDomainToProtoDomain(domain),
	}), nil
}

func (s *adminAPIService) CreateDomain(ctx context.Context, in *connect.Request[pb.CreateDomainRequest]) (*connect.Response[pb.Domain], error) {
	domain, err := s.dm.CreateDomain(ctx, in.Msg.Domain)
	if err != nil {
		return nil, err
	}

	return connect.NewResponse(dbDomainToProtoDomain(domain)), nil
}

func (s *adminAPIService) RegenerateDomainKey(ctx context.Context, in *connect.Request[pb.RegenerateDomainKeyRequest]) (*connect.Response[pb.Domain], error) {
	domain, err := s.dm.RegenerateDomainKey(ctx, in.Msg.Domain)
	if err != nil {
		return nil, err
	}

	return connect.NewResponse(dbDomainToProtoDomain(domain)), nil
}

func dbDomainToProtoDomain(in sqlc.Domain) *pb.Domain {
	return &pb.Domain{
		Domain:     in.Domain,
		Key:        in.Key,
		DkimPubKey: in.DkimPublicKey,
	}
}
