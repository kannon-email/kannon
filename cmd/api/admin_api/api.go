package admin_api

import (
	"context"
	"database/sql"

	"github.com/sirupsen/logrus"
	"google.golang.org/protobuf/types/known/emptypb"
	"kannon.gyozatech.dev/generated/pb"
	"kannon.gyozatech.dev/generated/sqlc"
	"kannon.gyozatech.dev/internal/domains"
)

type adminApiService struct {
	dm domains.DomainManager
}

func (s *adminApiService) GetDomains(ctx context.Context, in *emptypb.Empty) (*pb.GetDomainsResponse, error) {
	domains, err := s.dm.GetAllDomains()
	if err != nil {
		return nil, err
	}

	res := pb.GetDomainsResponse{}
	for _, domain := range domains {
		res.Domains = append(res.Domains, dbDomainToProtoDomain(domain))
	}
	return &res, nil
}

func (s *adminApiService) CreateDomain(ctx context.Context, in *pb.CreateDomainRequest) (*pb.Domain, error) {
	domain, err := s.dm.CreateDomain(in.Domain)
	if err != nil {
		return nil, err
	}

	return dbDomainToProtoDomain(domain), nil
}

func (s *adminApiService) RegenerateDomainKey(ctx context.Context, in *pb.RegenerateDomainKeyRequest) (*pb.Domain, error) {
	return nil, nil
}

func CreateAdminAPIService(db *sql.DB) (pb.ApiServer, error) {
	logrus.Infof("Connected to db\n")
	dm, err := domains.NewDomainManager(db)
	if err != nil {
		return nil, err
	}
	api := adminApiService{
		dm: dm,
	}

	return &api, nil
}

func dbDomainToProtoDomain(in sqlc.Domain) *pb.Domain {
	return &pb.Domain{
		Domain:     in.Domain,
		Key:        in.Key,
		DkimPubKey: in.DkimPublicKey,
	}
}
