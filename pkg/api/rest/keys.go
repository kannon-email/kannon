package rest

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/kannon-email/kannon/internal/apikeys"
	"github.com/kannon-email/kannon/internal/authz"
	"github.com/kannon-email/kannon/internal/values"
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
		if err := query(r, false); err != nil {
			return err
		}
		domain, err := values.Parse(r.PathValue(domainParameter))
		if err != nil {
			return problemError(400, "malformed_request")
		}
		var input APIKeyCreate
		if err := decode(w, r, &input); err != nil {
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
			if err := query(r, false); err != nil {
				return err
			}
			ref, err := apikeys.ParseKeyRef(r.PathValue(domainParameter), r.PathValue("key_id"))
			if err != nil {
				return problemError(400, "malformed_request")
			}
			var k *apikeys.APIKey
			if r.Method == http.MethodPatch {
				var input APIKeyRevoke
				if err := decode(w, r, &input); err != nil {
					return err
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
		page := apikeys.Pagination{Limit: limit + 1}
		if r.URL.Query().Has(cursorParameter) {
			raw, e := base64.RawURLEncoding.DecodeString(r.URL.Query().Get(cursorParameter))
			var c keyCursor
			if e != nil || json.Unmarshal(raw, &c) != nil || c.Collection != "v1beta1:api-keys:created_at,id:desc" || c.Domain != domain.String() || c.Time.IsZero() || strings.TrimSpace(c.ID) == "" {
				return problemError(400, "invalid_cursor")
			}
			page.Before = &c.Time
			page.BeforeID = c.ID
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
