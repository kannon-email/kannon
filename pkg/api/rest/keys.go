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
	mux.HandleFunc("POST /v1beta1/domains/{domain}/api-keys", func(w http.ResponseWriter, r *http.Request) {
		if !query(w, r, false) {
			return
		}
		domain, err := values.Parse(r.PathValue("domain"))
		if err != nil {
			WriteProblem(w, r, 400, "malformed_request")
			return
		}
		var input APIKeyCreate
		if !decode(w, r, &input) {
			return
		}
		roles := []authz.RoleName{}
		for _, role := range input.Roles {
			roles = append(roles, authz.RoleName(role))
		}
		result, err := service.IssueKey(r.Context(), domain, input.Name, roles, input.ExpiresAt)
		if err != nil {
			writeError(w, r, err)
			return
		}
		k := keyWire(result.Key)
		w.Header().Set("Location", "/v1beta1/domains/"+domain.String()+"/api-keys/"+k.Id)
		writeJSON(w, 201, APIKeyCreated{Id: k.Id, Domain: k.Domain, Name: k.Name, Prefix: k.Prefix, Roles: k.Roles, CreatedAt: k.CreatedAt, ExpiresAt: k.ExpiresAt, RevokedAt: k.RevokedAt, Status: APIKeyCreatedStatus(k.Status), Secret: result.PlaintextKey})
	})
	for _, method := range []string{"GET", "PATCH"} {
		mux.HandleFunc(method+" /v1beta1/domains/{domain}/api-keys/{key_id}", func(w http.ResponseWriter, r *http.Request) {
			if !query(w, r, false) {
				return
			}
			ref, err := apikeys.ParseKeyRef(r.PathValue("domain"), r.PathValue("key_id"))
			if err != nil {
				WriteProblem(w, r, 400, "malformed_request")
				return
			}
			var k *apikeys.APIKey
			if r.Method == "PATCH" {
				var input APIKeyRevoke
				if !decode(w, r, &input) {
					return
				}
				if input.Status != Revoked {
					WriteProblem(w, r, 422, "validation_failed")
					return
				}
				k, err = service.DeactivateKey(r.Context(), ref)
			} else {
				k, err = service.GetKey(r.Context(), ref)
			}
			if err != nil {
				writeError(w, r, err)
				return
			}
			writeJSON(w, 200, keyWire(k))
		})
	}
	mux.HandleFunc("GET /v1beta1/domains/{domain}/api-keys", func(w http.ResponseWriter, r *http.Request) {
		if !query(w, r, true) {
			return
		}
		domain, err := values.Parse(r.PathValue("domain"))
		if err != nil {
			WriteProblem(w, r, 400, "malformed_request")
			return
		}
		limit := 100
		if r.URL.Query().Has("limit") {
			limit, err = strconv.Atoi(r.URL.Query().Get("limit"))
			if err != nil || limit < 1 || limit > 1000 {
				WriteProblem(w, r, 400, "invalid_query_parameter")
				return
			}
		}
		page := apikeys.Pagination{Limit: limit + 1}
		if r.URL.Query().Has("cursor") {
			raw, e := base64.RawURLEncoding.DecodeString(r.URL.Query().Get("cursor"))
			var c keyCursor
			if e != nil || json.Unmarshal(raw, &c) != nil || c.Collection != "v1beta1:api-keys:created_at,id:desc" || c.Domain != domain.String() || c.Time.IsZero() || strings.TrimSpace(c.ID) == "" {
				WriteProblem(w, r, 400, "invalid_cursor")
				return
			}
			page.Before = &c.Time
			page.BeforeID = c.ID
		}
		keys, err := service.ListPage(r.Context(), domain, page)
		if err != nil {
			writeError(w, r, err)
			return
		}
		result := APIKeyPage{Items: []APIKey{}}
		if len(keys) > limit {
			last := keys[limit-1]
			raw, _ := json.Marshal(keyCursor{Collection: "v1beta1:api-keys:created_at,id:desc", Domain: domain.String(), Time: last.CreatedAt(), ID: last.ID().String()})
			c := base64.RawURLEncoding.EncodeToString(raw)
			result.NextCursor = &c
			keys = keys[:limit]
		}
		for _, k := range keys {
			result.Items = append(result.Items, keyWire(k))
		}
		writeJSON(w, 200, result)
	})
}
