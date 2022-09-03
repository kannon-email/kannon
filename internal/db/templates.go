package sqlc

import pb "github.com/ludusrusso/kannon/proto/kannon/admin/apiv1"

func (t Template) Pb() *pb.Template {
	return &pb.Template{
		TemplateId: t.TemplateID,
		Html:       t.Html,
		Title:      t.Title,
		Type:       string(t.Type),
	}
}
