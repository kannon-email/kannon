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
	mux.HandleFunc("POST /v1beta1/domains/{domain}/templates", func(w http.ResponseWriter, r *http.Request) {
		if !query(w, r, false) {
			return
		}
		domain, err := values.Parse(r.PathValue(domainParameter))
		if err != nil {
			WriteProblem(w, r, 400, "malformed_request")
			return
		}
		var input TemplatePatch
		if !decode(w, r, &input) {
			return
		}
		if input.Html == nil || *input.Html == "" || input.Title == nil {
			WriteProblem(w, r, 422, "validation_failed")
			return
		}
		t, err := service.CreateTemplate(r.Context(), domain, *input.Html, *input.Title)
		if err != nil {
			writeError(w, r, err)
			return
		}
		w.Header().Set("Location", "/v1beta1/domains/"+domain.String()+"/templates/"+t.TemplateID())
		writeJSON(w, 201, templateWire(t))
	})
	for _, method := range []string{"GET", http.MethodPatch, http.MethodDelete} {
		mux.HandleFunc(method+" /v1beta1/domains/{domain}/templates/{template_id}", func(w http.ResponseWriter, r *http.Request) {
			if !query(w, r, false) {
				return
			}
			domain, err := values.Parse(r.PathValue(domainParameter))
			if err != nil {
				WriteProblem(w, r, 400, "malformed_request")
				return
			}
			id := r.PathValue("template_id")
			var t *templates.Template
			switch r.Method {
			case http.MethodPatch:
				var input TemplatePatch
				if !decode(w, r, &input) {
					return
				}
				if (input.Html == nil && input.Title == nil) || (input.Html != nil && *input.Html == "") {
					WriteProblem(w, r, 422, "validation_failed")
					return
				}
				t, err = service.PatchTemplate(r.Context(), domain, id, input.Html, input.Title)
			case http.MethodDelete:
				t, err = service.DeleteTemplate(r.Context(), domain, id)
			default:
				t, err = service.GetTemplate(r.Context(), domain, id)
			}
			if err != nil {
				writeError(w, r, err)
				return
			}
			if r.Method == http.MethodDelete {
				w.WriteHeader(http.StatusNoContent)
				return
			}
			writeJSON(w, 200, templateWire(t))
		})
	}
	mux.HandleFunc("GET /v1beta1/domains/{domain}/templates", func(w http.ResponseWriter, r *http.Request) {
		if !query(w, r, true) {
			return
		}
		domain, err := values.Parse(r.PathValue(domainParameter))
		if err != nil {
			WriteProblem(w, r, 400, "malformed_request")
			return
		}
		limit := 100
		if r.URL.Query().Has(limitParameter) {
			limit, err = strconv.Atoi(r.URL.Query().Get(limitParameter))
			if err != nil || limit < 1 || limit > 1000 {
				WriteProblem(w, r, 400, "invalid_query_parameter")
				return
			}
		}
		const collection = "v1beta1:templates:persistent:id:asc"
		after := ""
		if r.URL.Query().Has(cursorParameter) {
			raw, e := base64.RawURLEncoding.DecodeString(r.URL.Query().Get(cursorParameter))
			var c templateCursor
			if e != nil || json.Unmarshal(raw, &c) != nil || c.Collection != collection || c.Domain != domain.String() || c.ID == "" {
				WriteProblem(w, r, 400, "invalid_cursor")
				return
			}
			after = c.ID
		}
		items, err := service.ListPage(r.Context(), domain, after, uint(limit+1))
		if err != nil {
			writeError(w, r, err)
			return
		}
		result := TemplateSummaryPage{Items: []TemplateSummary{}}
		if len(items) > limit {
			raw, err := json.Marshal(templateCursor{Collection: collection, Domain: domain.String(), ID: items[limit-1].TemplateID()})
			if err != nil {
				writeError(w, r, err)
				return
			}
			cursor := base64.RawURLEncoding.EncodeToString(raw)
			result.NextCursor = &cursor
			items = items[:limit]
		}
		for _, t := range items {
			result.Items = append(result.Items, TemplateSummary{Id: t.TemplateID(), Domain: t.DomainName().String(), Title: t.Title(), Lifetime: TemplateSummaryLifetimePersistent})
		}
		writeJSON(w, 200, result)
	})
}
