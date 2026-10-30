// Package rest implements the versioned HTTP adapter. Application modules own authorization.
package rest

//go:generate go run ./generate

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/url"

	"strconv"
	"strings"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/kannon-email/kannon/internal/admintoken"
	"github.com/kannon-email/kannon/internal/apikeys"
	"github.com/kannon-email/kannon/internal/authz"
	"github.com/kannon-email/kannon/internal/domains"
	"github.com/kannon-email/kannon/internal/intake"
	"github.com/kannon-email/kannon/internal/stats"
	"github.com/kannon-email/kannon/internal/templates"
	"github.com/kannon-email/kannon/internal/tracking"
	"github.com/kannon-email/kannon/internal/values"
)

const (
	statusKey       = "status"
	limitParameter  = "limit"
	cursorParameter = "cursor"
)

// NewHandler exposes Domains and readiness. ready must check the configured infrastructure.
// The supplied service is shared with other transports; no HTTP types escape this package.
func NewHandler(service *domains.Service, token admintoken.Token, ready func(context.Context) error, credentials ...*apikeys.Service) http.Handler {
	return newHandler(service, token, ready, nil, nil, nil, credentials...)
}

// NewHandlerWithStats includes retained event operations alongside Domain operations.
func NewHandlerWithStats(service *domains.Service, token admintoken.Token, ready func(context.Context) error, events *stats.Service, credentials ...*apikeys.Service) http.Handler {
	return newHandler(service, token, ready, events, nil, nil, credentials...)
}

// NewHandlerWithTemplates exposes the Template lifecycle alongside Domains and credentials.
func NewHandlerWithTemplates(service *domains.Service, token admintoken.Token, ready func(context.Context) error, keys *apikeys.Service, content *templates.Service) http.Handler {
	return newHandler(service, token, ready, nil, content, nil, keys)
}

// NewHandlerWithStatsAndTemplates exposes both retained events and Template lifecycle operations.
func NewHandlerWithStatsAndTemplates(service *domains.Service, token admintoken.Token, ready func(context.Context) error, events *stats.Service, keys *apikeys.Service, content *templates.Service) http.Handler {
	return newHandler(service, token, ready, events, content, nil, keys)
}

// NewHandlerWithBatches includes foundational intake alongside existing REST operations.
func NewHandlerWithBatches(service *domains.Service, token admintoken.Token, ready func(context.Context) error, inline *intake.InlineService, events *stats.Service, content *templates.Service, credentials ...*apikeys.Service) http.Handler {
	return newHandler(service, token, ready, events, content, inline, credentials...)
}

func newHandler(service *domains.Service, token admintoken.Token, ready func(context.Context) error, events *stats.Service, content *templates.Service, inline *intake.InlineService, credentials ...*apikeys.Service) http.Handler {
	var keys *apikeys.Service
	if len(credentials) > 0 {
		keys = credentials[0]
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1beta1/health", func(w http.ResponseWriter, r *http.Request) {
		if ready == nil || ready(r.Context()) != nil {
			WriteProblem(w, r, 503, "service_unavailable")
			return
		}
		writeJSON(w, 200, map[string]string{statusKey: "ready"})
	})
	protected := http.NewServeMux()
	if inline != nil {
		registerBatches(protected, inline)
	}
	if events != nil {
		registerEvents(protected, events)
	}
	if content != nil {
		registerTemplates(protected, content)
	}
	if keys != nil {
		registerKeys(protected, keys)
	}
	protected.HandleFunc("POST /v1beta1/domains", func(w http.ResponseWriter, r *http.Request) {
		if !query(w, r, false) {
			return
		}
		var input DomainCreate
		if !decode(w, r, &input) {
			return
		}
		name, err := values.Parse(input.Domain)
		if err != nil {
			WriteProblem(w, r, 422, "validation_failed")
			return
		}
		d, err := service.CreateDomain(r.Context(), name)
		if err != nil {
			writeError(w, r, err)
			return
		}
		w.Header().Set("Location", "/v1beta1/domains/"+d.Domain())
		writeJSON(w, 201, domainWire(d))
	})
	protected.HandleFunc("GET /v1beta1/domains", func(w http.ResponseWriter, r *http.Request) {
		if !query(w, r, true) {
			return
		}
		limit := 100
		if r.URL.Query().Has(limitParameter) {
			n, err := strconv.Atoi(r.URL.Query().Get(limitParameter))
			if err != nil || n < 1 || n > 1000 {
				WriteProblem(w, r, 400, "invalid_query_parameter")
				return
			}
			limit = n
		}
		after := ""
		if r.URL.Query().Has(cursorParameter) {
			b, err := base64.RawURLEncoding.DecodeString(r.URL.Query().Get(cursorParameter))
			prefix := "v1beta1:domains:name:"
			if err != nil || !strings.HasPrefix(string(b), prefix) {
				WriteProblem(w, r, 400, "invalid_cursor")
				return
			}
			after = strings.TrimPrefix(string(b), prefix)
			if _, err = values.Parse(after); err != nil {
				WriteProblem(w, r, 400, "invalid_cursor")
				return
			}
		}
		all, err := service.ListPage(r.Context(), after, limit+1)
		if err != nil {
			writeError(w, r, err)
			return
		}

		items := []Domain{}
		next := ""
		for _, d := range all {
			if d.Domain() <= after {
				continue
			}
			if len(items) == limit {
				next = base64.RawURLEncoding.EncodeToString([]byte("v1beta1:domains:name:" + items[len(items)-1].Domain))
				break
			}
			items = append(items, domainWire(d))
		}
		var cursor *string
		if next != "" {
			cursor = &next
		}
		writeJSON(w, 200, DomainPage{Items: items, NextCursor: cursor})
	})
	protected.HandleFunc("GET /v1beta1/domains/{domain}", func(w http.ResponseWriter, r *http.Request) {
		if !query(w, r, false) {
			return
		}
		name, err := values.Parse(r.PathValue("domain"))
		if err != nil {
			WriteProblem(w, r, 400, "malformed_request")
			return
		}
		d, err := service.GetDomain(r.Context(), name)
		if err != nil {
			writeError(w, r, err)
			return
		}
		writeJSON(w, 200, domainWire(d))
	})
	protected.HandleFunc("PATCH /v1beta1/domains/{domain}", func(w http.ResponseWriter, r *http.Request) {
		if !query(w, r, false) {
			return
		}
		name, err := values.Parse(r.PathValue("domain"))
		if err != nil {
			WriteProblem(w, r, 400, "malformed_request")
			return
		}
		var input DomainPatch
		if !decode(w, r, &input) {
			return
		}
		p := tracking.Policy{Opens: tracking.Mode(input.Tracking.Opens), Links: tracking.Mode(input.Tracking.Links)}
		_, ok := p.Opens.Rank()
		_, ok2 := p.Links.Rank()
		if !ok || !ok2 {
			WriteProblem(w, r, 422, "validation_failed")
			return
		}
		d, err := service.SetTrackingPolicy(r.Context(), name, p)
		if err != nil {
			writeError(w, r, err)
			return
		}
		writeJSON(w, 200, domainWire(d))
	})
	mux.Handle("/v1beta1/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		parts := strings.Split(r.Header.Get("Authorization"), " ")
		if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
			w.Header().Set("WWW-Authenticate", "Bearer")
			WriteProblem(w, r, 401, "unauthenticated")
			return
		}
		p, err := token.Authenticate(parts[1])
		if err != nil && keys != nil {
			p, err = keys.Authenticate(r.Context(), parts[1])
		}
		if err != nil {
			w.Header().Set("WWW-Authenticate", "Bearer")
			WriteProblem(w, r, 401, "unauthenticated")
			return
		}
		if raw := r.Header.Get("X-Kannon-Attribution"); raw != "" {
			a, err := authz.ParseAttribution(raw)
			if err != nil {
				WriteProblem(w, r, 400, "invalid_attribution")
				return
			}
			p = p.WithAttribution(a)
		}
		protected.ServeHTTP(w, r.WithContext(authz.NewContext(r.Context(), p)))
	}))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := make([]byte, 16)
		_, _ = rand.Read(id)
		w.Header().Set("X-Request-ID", base64.RawURLEncoding.EncodeToString(id))
		mux.ServeHTTP(w, r)
	})
}

func domainWire(d *domains.Domain) Domain {
	p := d.TrackingPolicy()
	return Domain{Domain: d.Domain(), DkimPublicKey: d.DkimPublicKey(), Tracking: TrackingPolicy{Opens: TrackingMode(p.Opens), Links: TrackingMode(p.Links)}}
}
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		slog.Debug("cannot write REST response", "err", err)
	}
}

// WriteProblem writes a sanitized RFC 9457 response with a stable machine code.
func WriteProblem(w http.ResponseWriter, r *http.Request, status int, code string) {
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(map[string]any{"type": "urn:kannon:problem:" + code, "title": http.StatusText(status), statusKey: status, "code": code, "request_id": w.Header().Get("X-Request-ID")}); err != nil {
		slog.Debug("cannot write Problem Details", "err", err)
	}
}
func writeError(w http.ResponseWriter, r *http.Request, err error) {
	var pg *pgconn.PgError
	switch {
	case errors.Is(err, authz.ErrForbidden), errors.Is(err, authz.ErrNoPrincipal):
		WriteProblem(w, r, 403, "forbidden")
	case errors.Is(err, apikeys.ErrInvalidRoles), errors.Is(err, apikeys.ErrInvalidName):
		WriteProblem(w, r, 422, "validation_failed")
	case errors.Is(err, apikeys.ErrExpiryNotFuture):
		WriteProblem(w, r, 422, "expiry_not_future")
	case errors.Is(err, templates.ErrTemplateNotFound), errors.Is(err, apikeys.ErrKeyNotFound), errors.Is(err, domains.ErrDomainNotFound):
		WriteProblem(w, r, 404, "not_found")
	case errors.As(err, &pg) && pg.Code == "23505":
		WriteProblem(w, r, 409, "domain_already_exists")
	default:
		WriteProblem(w, r, 500, "internal_error")
	}
}
func decode(w http.ResponseWriter, r *http.Request, v any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 10<<20)
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	err := d.Decode(v)
	if err == nil {
		var tail any
		if d.Decode(&tail) == io.EOF {
			return true
		}
	}
	var tooLarge *http.MaxBytesError
	if errors.As(err, &tooLarge) {
		WriteProblem(w, r, 413, "request_body_too_large")
	} else {
		WriteProblem(w, r, 400, "malformed_request")
	}
	return false
}
func query(w http.ResponseWriter, r *http.Request, list bool) bool {
	q, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil {
		WriteProblem(w, r, 400, "invalid_query_parameter")
		return false
	}
	for k, v := range q {
		if !list || (k != limitParameter && k != cursorParameter) {
			WriteProblem(w, r, 400, "unknown_query_parameter")
			return false
		}
		if len(v) != 1 {
			WriteProblem(w, r, 400, "invalid_query_parameter")
			return false
		}
	}
	return true
}
