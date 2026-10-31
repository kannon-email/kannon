package rest

import (
	"net/http"

	"github.com/kannon-email/kannon/internal/templates"
)

func templateWire(t *templates.Template) Template {
	lifetime := TemplateLifetimePersistent
	if t.Type() == templates.TypeTransient {
		lifetime = TemplateLifetimeTransient
	}
	return Template{Id: t.TemplateID(), Domain: t.DomainName().String(), Title: t.Title(), Html: t.Html(), Lifetime: lifetime}
}

func registerTemplates(mux *http.ServeMux, service *templates.Service) {
	mux.Handle("POST /v1beta1/domains/{domain}/templates", handler(func(w http.ResponseWriter, r *http.Request) error {
		if _, err := parseQuery(r.URL.RawQuery); err != nil {
			return err
		}
		domain, err := parseDomainPath(r)
		if err != nil {
			return err
		}
		input, err := decodeBody[TemplatePatch](r.Body)
		if err != nil {
			return err
		}
		if input.Html == nil || *input.Html == "" || input.Title == nil {
			return problemError(422, "validation_failed")
		}
		t, err := service.CreateTemplate(r.Context(), domain, *input.Html, *input.Title)
		if err != nil {
			return err
		}
		w.Header().Set("Location", "/v1beta1/domains/"+domain.String()+"/templates/"+t.TemplateID())
		writeJSON(w, 201, templateWire(t))
		return nil
	}))
	for _, method := range []string{"GET", http.MethodPatch, http.MethodDelete} {
		mux.Handle(method+" /v1beta1/domains/{domain}/templates/{template_id}", handler(func(w http.ResponseWriter, r *http.Request) error {
			if _, err := parseQuery(r.URL.RawQuery); err != nil {
				return err
			}
			domain, err := parseDomainPath(r)
			if err != nil {
				return err
			}
			id := r.PathValue("template_id")
			var t *templates.Template
			switch r.Method {
			case http.MethodPatch:
				input, decodeErr := decodeBody[TemplatePatch](r.Body)
				if decodeErr != nil {
					return decodeErr
				}
				if (input.Html == nil && input.Title == nil) || (input.Html != nil && *input.Html == "") {
					return problemError(422, "validation_failed")
				}
				t, err = service.PatchTemplate(r.Context(), domain, id, input.Html, input.Title)
			case http.MethodDelete:
				t, err = service.DeleteTemplate(r.Context(), domain, id)
			default:
				t, err = service.GetTemplate(r.Context(), domain, id)
			}
			if err != nil {
				return err
			}
			if r.Method == http.MethodDelete {
				w.WriteHeader(http.StatusNoContent)
				return nil
			}
			writeJSON(w, 200, templateWire(t))
			return nil
		}))
	}
	mux.Handle("GET /v1beta1/domains/{domain}/templates", handler(func(w http.ResponseWriter, r *http.Request) error {
		q, err := parseQuery(r.URL.RawQuery, limitParameter, cursorParameter)
		if err != nil {
			return err
		}
		domain, err := parseDomainPath(r)
		if err != nil {
			return err
		}
		page, err := readPage(q, func(id string) bool { return id != "" }, "templates:persistent:id:asc", domain.String())
		if err != nil {
			return err
		}
		items, err := service.ListPage(r.Context(), domain, page.keyset())
		if err != nil {
			return err
		}
		items, next, err := writePage(page, items, (*templates.Template).TemplateID)
		if err != nil {
			return err
		}
		result := TemplateSummaryPage{Items: []TemplateSummary{}, NextCursor: next}
		for _, t := range items {
			result.Items = append(result.Items, TemplateSummary{Id: t.TemplateID(), Domain: t.DomainName().String(), Title: t.Title(), Lifetime: TemplateSummaryLifetimePersistent})
		}
		writeJSON(w, 200, result)
		return nil
	}))
}
