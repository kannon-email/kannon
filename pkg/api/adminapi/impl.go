package adminapi

import (
	"context"

	"github.com/kannon-email/kannon/internal/apikeys"
	sqlc "github.com/kannon-email/kannon/internal/db"
	"github.com/kannon-email/kannon/internal/domains"
	"github.com/kannon-email/kannon/internal/templates"

	pb "github.com/kannon-email/kannon/proto/kannon/admin/apiv1"
)

type adminAPIService struct {
	domains   domains.Repository
	templates templates.Repository
	apiKeys   *apikeys.Service
	q         *sqlc.Queries
}

func (s *adminAPIService) GetDomains(ctx context.Context, in *pb.GetDomainsReq) (*pb.GetDomainsResponse, error) {
	all, err := s.domains.List(ctx)
	if err != nil {
		return nil, err
	}

	res := pb.GetDomainsResponse{}
	for _, d := range all {
		res.Domains = append(res.Domains, d.Pb())
	}
	return &res, nil
}

func (s *adminAPIService) GetDomain(ctx context.Context, in *pb.GetDomainReq) (*pb.GetDomainRes, error) {
	d, err := s.domains.FindByName(ctx, in.Domain)
	if err != nil {
		return nil, err
	}

	return &pb.GetDomainRes{
		Domain: d.Pb(),
	}, nil
}

func (s *adminAPIService) CreateDomain(ctx context.Context, in *pb.CreateDomainRequest) (*pb.Domain, error) {
	d, err := domains.New(in.Domain)
	if err != nil {
		return nil, err
	}
	if err := s.domains.Create(ctx, d); err != nil {
		return nil, err
	}
	return d.Pb(), nil
}
