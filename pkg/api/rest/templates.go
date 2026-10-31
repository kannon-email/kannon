package rest

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/kannon-email/kannon/internal/templates"
	"github.com/kannon-email/kannon/internal/values"
)

func templateWire(t *templates.Template) Template {
	lifetime := TemplateLifetimePersistent
	if t.Type() == templates.TypeTransient {
		lifetime = TemplateLifetimeTransient
	}
	return Template{Id: t.TemplateID(), Domain: t.DomainName().String(), Title: t.Title(), Html: t.Html(), Lifetime: lifetime}
}

type templateCursor struct {
	Collection string
	Domain     string
	ID         string
}

func registerTemplates(mux *http.ServeMux, service *templates.Service) {
	mux.Handle("POST /v1beta1/domains/{domain}/templates", handler(func(w http.ResponseWriter, r *http.Request) error {
		if err := query(r, false); err != nil {
			return err
		}
		domain, err := values.Parse(r.PathValue(domainParameter))
		if err != nil {
			return problemError(400, "malformed_request")
		}
		var input TemplatePatch
		if err := decode(w, r, &input); err != nil {
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
			if err := query(r, false); err != nil {
				return err
			}
			domain, err := values.Parse(r.PathValue(domainParameter))
			if err != nil {
				return problemError(400, "malformed_request")
			}
			id := r.PathValue("template_id")
			var t *templates.Template
			switch r.Method {
			case http.MethodPatch:
				var input TemplatePatch
				if err := decode(w, r, &input); err != nil {
					return err
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
		if err := query(r, true); err != nil {
			return err
		}
		domain, err := values.Parse(r.PathValue(domainParameter))
		if err != nil {
			return problemError(400, "malformed_request")
		}
		limit := 100
		if r.URL.Query().Has(limitParameter) {
			limit, err = strconv.Atoi(r.URL.Query().Get(limitParameter))
			if err != nil || limit < 1 || limit > 1000 {
				return problemError(400, "invalid_query_parameter")
			}
		}
		const collection = "v1beta1:templates:persistent:id:asc"
		after := ""
		if r.URL.Query().Has(cursorParameter) {
			raw, e := base64.RawURLEncoding.DecodeString(r.URL.Query().Get(cursorParameter))
			var c templateCursor
			if e != nil || json.Unmarshal(raw, &c) != nil || c.Collection != collection || c.Domain != domain.String() || c.ID == "" {
				return problemError(400, "invalid_cursor")
			}
			after = c.ID
		}
		items, err := service.ListPage(r.Context(), domain, after, uint(limit+1))
		if err != nil {
			return err
		}
		result := TemplateSummaryPage{Items: []TemplateSummary{}}
		if len(items) > limit {
			raw, err := json.Marshal(templateCursor{Collection: collection, Domain: domain.String(), ID: items[limit-1].TemplateID()})
			if err != nil {
				return err
			}
			cursor := base64.RawURLEncoding.EncodeToString(raw)
			result.NextCursor = &cursor
			items = items[:limit]
		}
		for _, t := range items {
			result.Items = append(result.Items, TemplateSummary{Id: t.TemplateID(), Domain: t.DomainName().String(), Title: t.Title(), Lifetime: TemplateSummaryLifetimePersistent})
		}
		writeJSON(w, 200, result)
		return nil
	}))
}
