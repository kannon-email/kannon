package adminapi

import (
	"kannon.gyozatech.dev/generated/pb"
	"kannon.gyozatech.dev/generated/sqlc"
	"kannon.gyozatech.dev/internal/domains"
)

func CreateAdminAPIService(q *sqlc.Queries) pb.ApiServer {
	dm := domains.NewDomainManager(q)
	return &adminAPIService{
		dm: dm,
	}
}
