package rest

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/kannon-email/kannon/internal/apikeys"
	"github.com/kannon-email/kannon/internal/authz"
)

func keyWire(k *apikeys.APIKey) APIKey {
	roles := []Role{}
	for _, r := range k.Roles() {
		roles = append(roles, Role(r))
	}
	status := APIKeyStatusActive
	if !k.IsActiveStatus() {
		status = APIKeyStatusRevoked
	} else if k.IsExpired() {
		status = APIKeyStatusExpired
	}
	return APIKey{Id: k.ID().String(), Domain: k.Domain(), Name: k.Name(), Prefix: k.KeyPrefix(), Roles: roles, Status: status, CreatedAt: k.CreatedAt().UTC(), ExpiresAt: k.ExpiresAt(), RevokedAt: k.DeactivatedAt()}
}

type keyCursor struct {
	Collection string
	Domain     string
	Time       time.Time
	ID         string
}

func registerKeys(mux *http.ServeMux, service *apikeys.Service) {
	mux.Handle("POST /v1beta1/domains/{domain}/api-keys", handler(func(w http.ResponseWriter, r *http.Request) error {
		if _, err := parseQuery(r.URL.RawQuery); err != nil {
			return err
		}
		domain, err := parseDomainPath(r)
		if err != nil {
			return err
		}
		input, err := decodeBody[APIKeyCreate](r.Body)
		if err != nil {
			return err
		}
		roles := []authz.RoleName{}
		for _, role := range input.Roles {
			roles = append(roles, authz.RoleName(role))
		}
		result, err := service.IssueKey(r.Context(), domain, input.Name, roles, input.ExpiresAt)
		if err != nil {
			return err
		}
		k := keyWire(result.Key)
		w.Header().Set("Location", "/v1beta1/domains/"+domain.String()+"/api-keys/"+k.Id)
		writeJSON(w, 201, APIKeyCreated{Id: k.Id, Domain: k.Domain, Name: k.Name, Prefix: k.Prefix, Roles: k.Roles, CreatedAt: k.CreatedAt, ExpiresAt: k.ExpiresAt, RevokedAt: k.RevokedAt, Status: APIKeyCreatedStatus(k.Status), Secret: result.PlaintextKey})
		return nil
	}))
	for _, method := range []string{"GET", http.MethodPatch} {
		mux.Handle(method+" /v1beta1/domains/{domain}/api-keys/{key_id}", handler(func(w http.ResponseWriter, r *http.Request) error {
			if _, err := parseQuery(r.URL.RawQuery); err != nil {
				return err
			}
			domain, err := parseDomainPath(r)
			if err != nil {
				return err
			}
			id, err := apikeys.ParseID(r.PathValue("key_id"))
			if err != nil {
				return problemError(400, "malformed_request")
			}
			ref := apikeys.NewKeyRef(domain, id)
			var k *apikeys.APIKey
			if r.Method == http.MethodPatch {
				input, decodeErr := decodeBody[APIKeyRevoke](r.Body)
				if decodeErr != nil {
					return decodeErr
				}
				if input.Status != Revoked {
					return problemError(422, "validation_failed")
				}
				k, err = service.DeactivateKey(r.Context(), ref)
			} else {
				k, err = service.GetKey(r.Context(), ref)
			}
			if err != nil {
				return err
			}
			writeJSON(w, 200, keyWire(k))
			return nil
		}))
	}
	mux.Handle("GET /v1beta1/domains/{domain}/api-keys", handler(func(w http.ResponseWriter, r *http.Request) error {
		q, err := parseQuery(r.URL.RawQuery, limitParameter, cursorParameter)
		if err != nil {
			return err
		}
		domain, err := parseDomainPath(r)
		if err != nil {
			return err
		}
		page, limit, err := parseKeyPage(q, domain.String())
		if err != nil {
			return err
		}
		keys, err := service.ListPage(r.Context(), domain, page)
		if err != nil {
			return err
		}
		result := APIKeyPage{Items: []APIKey{}}
		if len(keys) > limit {
			last := keys[limit-1]
			raw, err := json.Marshal(keyCursor{Collection: "v1beta1:api-keys:created_at,id:desc", Domain: domain.String(), Time: last.CreatedAt(), ID: last.ID().String()})
			if err != nil {
				return err
			}
			c := base64.RawURLEncoding.EncodeToString(raw)
			result.NextCursor = &c
			keys = keys[:limit]
		}
		for _, k := range keys {
			result.Items = append(result.Items, keyWire(k))
		}
		writeJSON(w, 200, result)
		return nil
	}))
}

func parseKeyPage(q url.Values, domain string) (apikeys.Pagination, int, error) {
	limit, err := pageLimit(q)
	if err != nil {
		return apikeys.Pagination{}, 0, err
	}
	page := apikeys.Pagination{Limit: limit + 1}
	if q.Has(cursorParameter) {
		raw, e := base64.RawURLEncoding.DecodeString(q.Get(cursorParameter))
		var c keyCursor
		if e != nil || json.Unmarshal(raw, &c) != nil || c.Collection != "v1beta1:api-keys:created_at,id:desc" || c.Domain != domain || c.Time.IsZero() || strings.TrimSpace(c.ID) == "" {
			return apikeys.Pagination{}, 0, problemError(400, "invalid_cursor")
		}
		page.Before = &c.Time
		page.BeforeID = c.ID
	}
	return page, limit, nil
}
