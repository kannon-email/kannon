package main

import (
	"context"

	"github.com/sirupsen/logrus"
	"gorm.io/gorm"
	"smtp.ludusrusso.space/generated/proto"
	"smtp.ludusrusso.space/internal/db"
	"smtp.ludusrusso.space/internal/domains"
)

type apiService struct {
	dm domains.DomainManager
}

func (s *apiService) GetDomains(ctx context.Context, in *proto.Empty) (*proto.GetDomainsResponse, error) {
	domains, err := s.dm.GetAllDomains()
	if err != nil {
		return nil, err
	}

	res := proto.GetDomainsResponse{}
	for _, domain := range domains {
		res.Domains = append(res.Domains, dbDomainToProtoDomain(domain))
	}
	return &res, nil
}

func (s *apiService) CreateDomain(ctx context.Context, in *proto.CreateDomainRequest) (*proto.Domain, error) {
	domain, err := s.dm.CreateDomain(in.Domain)
	if err != nil {
		return nil, err
	}

	return dbDomainToProtoDomain(domain), nil
}

func (s *apiService) RegenerateDomainKey(ctx context.Context, in *proto.RegenerateDomainKeyRequest) (*proto.Domain, error) {
	return nil, nil
}

func createAPIService(db *gorm.DB) (proto.ApiServer, error) {
	logrus.Infof("Connected to db\n")
	dm, err := domains.NewDomainManager(db)
	if err != nil {
		return nil, err
	}
	api := apiService{
		dm: dm,
	}

	return &api, nil
}

func dbDomainToProtoDomain(in db.Domain) *proto.Domain {
	return &proto.Domain{
		Domain:     in.Domain,
		Key:        in.Key,
		DkimPubKey: in.DKIMKeys.PublicKey,
	}
}
