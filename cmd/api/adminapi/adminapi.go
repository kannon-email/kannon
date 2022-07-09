package adminapi

import (
	"github.com/ludusrusso/kannon/generated/pb"
	sqlc "github.com/ludusrusso/kannon/internal/db"
	"github.com/ludusrusso/kannon/internal/domains"
)

func CreateAdminAPIService(q *sqlc.Queries) pb.ApiServer {
	dm := domains.NewDomainManager(q)
	return &adminAPIService{
		dm: dm,
	}
}
