package rest

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/url"

	"github.com/kannon-email/kannon/internal/templates"
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
		after, limit, err := parseTemplatePage(q, domain.String())
		if err != nil {
			return err
		}
		items, err := service.ListPage(r.Context(), domain, after, uint(limit+1))
		if err != nil {
			return err
		}
		result := TemplateSummaryPage{Items: []TemplateSummary{}}
		if len(items) > limit {
			raw, err := json.Marshal(templateCursor{Collection: templateCollection, Domain: domain.String(), ID: items[limit-1].TemplateID()})
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

func parseTemplatePage(q url.Values, domain string) (string, int, error) {
	limit, err := pageLimit(q)
	if err != nil {
		return "", 0, err
	}
	after := ""
	if q.Has(cursorParameter) {
		raw, e := base64.RawURLEncoding.DecodeString(q.Get(cursorParameter))
		var c templateCursor
		if e != nil || json.Unmarshal(raw, &c) != nil || c.Collection != templateCollection || c.Domain != domain || c.ID == "" {
			return "", 0, problemError(400, "invalid_cursor")
		}
		after = c.ID
	}
	return after, limit, nil
}

const templateCollection = "v1beta1:templates:persistent:id:asc"
