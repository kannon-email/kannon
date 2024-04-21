package adminapi

import (
	sqlc "github.com/ludusrusso/kannon/internal/db"
	"github.com/ludusrusso/kannon/internal/domains"
	"github.com/ludusrusso/kannon/internal/templates"

	pb "github.com/ludusrusso/kannon/proto/kannon/admin/apiv1/apiv1connect"
)

func CreateAdminAPIService(q *sqlc.Queries) pb.ApiHandler {
	dm := domains.NewDomainManager(q)
	tm := templates.NewTemplateManager(q)
	return &adminAPIService{
		dm: dm,
		tm: tm,
	}
}
