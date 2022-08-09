package adminapi

import (
	"github.com/ludusrusso/kannon/generated/pb"
	sqlc "github.com/ludusrusso/kannon/internal/db"
	"github.com/ludusrusso/kannon/internal/domains"
	"github.com/ludusrusso/kannon/internal/templates"
)

func CreateAdminAPIService(q *sqlc.Queries) pb.ApiServer {
	dm := domains.NewDomainManager(q)
	tm := templates.NewTemplateManager(q)
	return &adminAPIService{
		dm: dm,
		tm: tm,
	}
}
