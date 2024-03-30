package template

import (
	"github.com/ludusrusso/kannon/pkg/values/fqdn"
	"github.com/ludusrusso/kannon/pkg/values/meta"
	"github.com/ludusrusso/kannon/pkg/values/ref"
)

type Type string

const (
	TemplateTypeTransient Type = "transient"
	TemplateTypeTemplate  Type = "template"
)

type Template struct {
	meta.Meta
	ref.Ref
	html  string
	tType Type
}

func NewTemplate(domain fqdn.FQDN, tType Type, title, description string) (Template, error) {
	ref, err := ref.NewRef("template", domain)
	if err != nil {
		return Template{}, err
	}

	return Template{
		Meta:  meta.NewMeta(title, description),
		Ref:   ref,
		tType: tType,
		html:  "",
	}, nil
}

func (t Template) Type() Type {
	return t.tType
}

func (t Template) HTML() string {
	return t.html
}

func (t *Template) SetHTML(html string) {
	t.html = html
	t.Update()
}
