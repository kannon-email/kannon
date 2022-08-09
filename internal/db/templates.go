package sqlc

import "github.com/ludusrusso/kannon/generated/pb"

func (t Template) Pb() *pb.Template {
	return &pb.Template{
		TemplateId: t.TemplateID,
		Html:       t.Html,
		Title:      t.Title,
		Type:       string(t.Type),
	}
}
