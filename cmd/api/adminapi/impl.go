package adminapi

import (
	"context"

	"kannon.gyozatech.dev/generated/pb"
	"kannon.gyozatech.dev/generated/sqlc"
	"kannon.gyozatech.dev/internal/domains"
)

type adminAPIService struct {
	dm domains.DomainManager
}

func (s *adminAPIService) GetDomains(ctx context.Context, in *pb.GetDomainsReq) (*pb.GetDomainsResponse, error) {
	domains, err := s.dm.GetAllDomains(ctx)
	if err != nil {
		return nil, err
	}

	res := pb.GetDomainsResponse{}
	for _, domain := range domains {
		res.Domains = append(res.Domains, dbDomainToProtoDomain(domain))
	}
	return &res, nil
}

func (s *adminAPIService) CreateDomain(ctx context.Context, in *pb.CreateDomainRequest) (*pb.Domain, error) {
	domain, err := s.dm.CreateDomain(ctx, in.Domain)
	if err != nil {
		return nil, err
	}

	return dbDomainToProtoDomain(domain), nil
}

func (s *adminAPIService) RegenerateDomainKey(ctx context.Context, in *pb.RegenerateDomainKeyRequest) (*pb.Domain, error) {
	domain, err := s.dm.RegenerateDomainKey(ctx, in.Domain)
	if err != nil {
		return nil, err
	}

	return dbDomainToProtoDomain(domain), nil
}

func dbDomainToProtoDomain(in sqlc.Domain) *pb.Domain {
	return &pb.Domain{
		Domain:     in.Domain,
		Key:        in.Key,
		DkimPubKey: in.DkimPublicKey,
	}
}
