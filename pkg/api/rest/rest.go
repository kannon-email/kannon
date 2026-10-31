// Package rest implements the versioned HTTP adapter. Application modules own authorization.
package rest

//go:generate go run ./generate

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"

	"strconv"
	"strings"

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
	mux.Handle("GET /v1beta1/health", handler(func(w http.ResponseWriter, r *http.Request) error {
		if ready == nil || ready(r.Context()) != nil {
			return problemError(503, "service_unavailable")
		}
		writeJSON(w, 200, map[string]string{statusKey: "ready"})
		return nil
	}))
	protected := http.NewServeMux()
	if inline != nil {
		registerBatches(protected, inline)
		registerDeliveries(protected, inline)
	}
	if events != nil {
		registerEvents(protected, events)
		registerAggregated(protected, events)
	}
	if content != nil {
		registerTemplates(protected, content)
	}
	if keys != nil {
		registerKeys(protected, keys)
	}
	protected.Handle("POST /v1beta1/domains", handler(func(w http.ResponseWriter, r *http.Request) error {
		if err := query(r, false); err != nil {
			return err
		}
		var input DomainCreate
		if err := decode(w, r, &input); err != nil {
			return err
		}
		name, err := values.Parse(input.Domain)
		if err != nil {
			return problemError(422, "validation_failed")
		}
		d, err := service.CreateDomain(r.Context(), name)
		if err != nil {
			return err
		}
		w.Header().Set("Location", "/v1beta1/domains/"+d.Domain())
		writeJSON(w, 201, domainWire(d))
		return nil
	}))
	protected.Handle("GET /v1beta1/domains", handler(func(w http.ResponseWriter, r *http.Request) error {
		if err := query(r, true); err != nil {
			return err
		}
		limit := 100
		if r.URL.Query().Has(limitParameter) {
			n, err := strconv.Atoi(r.URL.Query().Get(limitParameter))
			if err != nil || n < 1 || n > 1000 {
				return problemError(400, "invalid_query_parameter")
			}
			limit = n
		}
		after := ""
		if r.URL.Query().Has(cursorParameter) {
			b, err := base64.RawURLEncoding.DecodeString(r.URL.Query().Get(cursorParameter))
			prefix := "v1beta1:domains:name:"
			if err != nil || !strings.HasPrefix(string(b), prefix) {
				return problemError(400, "invalid_cursor")
			}
			after = strings.TrimPrefix(string(b), prefix)
			if _, err = values.Parse(after); err != nil {
				return problemError(400, "invalid_cursor")
			}
		}
		all, err := service.ListPage(r.Context(), after, limit+1)
		if err != nil {
			return err
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
		return nil
	}))
	protected.Handle("GET /v1beta1/domains/{domain}", handler(func(w http.ResponseWriter, r *http.Request) error {
		if err := query(r, false); err != nil {
			return err
		}
		name, err := values.Parse(r.PathValue(domainParameter))
		if err != nil {
			return problemError(400, "malformed_request")
		}
		d, err := service.GetDomain(r.Context(), name)
		if err != nil {
			return err
		}
		writeJSON(w, 200, domainWire(d))
		return nil
	}))
	protected.Handle("PATCH /v1beta1/domains/{domain}", handler(func(w http.ResponseWriter, r *http.Request) error {
		if err := query(r, false); err != nil {
			return err
		}
		name, err := values.Parse(r.PathValue(domainParameter))
		if err != nil {
			return problemError(400, "malformed_request")
		}
		var input DomainPatch
		if err := decode(w, r, &input); err != nil {
			return err
		}
		p := tracking.Policy{Opens: tracking.Mode(input.Tracking.Opens), Links: tracking.Mode(input.Tracking.Links)}
		_, ok := p.Opens.Rank()
		_, ok2 := p.Links.Rank()
		if !ok || !ok2 {
			return problemError(422, "validation_failed")
		}
		d, err := service.SetTrackingPolicy(r.Context(), name, p)
		if err != nil {
			return err
		}
		writeJSON(w, 200, domainWire(d))
		return nil
	}))
	mux.Handle("/v1beta1/", handler(func(w http.ResponseWriter, r *http.Request) error {
		parts := strings.Split(r.Header.Get("Authorization"), " ")
		if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
			return problemError(401, "unauthenticated")
		}
		p, err := token.Authenticate(parts[1])
		if err != nil && keys != nil {
			p, err = keys.Authenticate(r.Context(), parts[1])
		}
		if err != nil {
			return problemError(401, "unauthenticated")
		}
		if raw := r.Header.Get("X-Kannon-Attribution"); raw != "" {
			a, err := authz.ParseAttribution(raw)
			if err != nil {
				return problemError(400, "invalid_attribution")
			}
			p = p.WithAttribution(a)
		}
		protected.ServeHTTP(w, r.WithContext(authz.NewContext(r.Context(), p)))
		return nil
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

func decode(w http.ResponseWriter, r *http.Request, v any) error {
	r.Body = http.MaxBytesReader(w, r.Body, 10<<20)
	raw, readErr := io.ReadAll(r.Body)
	if readErr != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(readErr, &tooLarge) {
			return problemError(413, "request_body_too_large")
		} else {
			return problemError(400, "malformed_request")
		}
	}
	var tree any
	if json.Unmarshal(raw, &tree) == nil {
		if pointer, found := nullPointer(tree, ""); found {
			return &requestError{status: 422, code: "validation_failed", fields: []FieldError{{Code: "validation_failed", Pointer: pointer, Detail: "Null is not allowed"}}}
		}
	}
	d := json.NewDecoder(strings.NewReader(string(raw)))
	d.DisallowUnknownFields()
	err := d.Decode(v)
	if err == nil {
		var tail any
		err = d.Decode(&tail)
		if err == io.EOF {
			return nil
		}
	}
	var tooLarge *http.MaxBytesError
	if errors.As(err, &tooLarge) {
		return problemError(413, "request_body_too_large")
	} else {
		return problemError(400, "malformed_request")
	}
}
func query(r *http.Request, list bool) error {
	q, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil {
		return problemError(400, "invalid_query_parameter")
	}
	for k, v := range q {
		if !list || (k != limitParameter && k != cursorParameter) {
			return problemError(400, "unknown_query_parameter")
		}
		if len(v) != 1 {
			return problemError(400, "invalid_query_parameter")
		}
	}
	return nil
}

// Request schemas contain no nullable inputs. Check presence before decoding into
// Go pointers, which otherwise conflate explicit null with an omitted member.
func nullPointer(v any, path string) (string, bool) {
	if v == nil {
		return path, true
	}
	switch x := v.(type) {
	case map[string]any:
		for k, v := range x {
			escaped := strings.ReplaceAll(strings.ReplaceAll(k, "~", "~0"), "/", "~1")
			if p, ok := nullPointer(v, path+"/"+escaped); ok {
				return p, true
			}
		}
	case []any:
		for i, v := range x {
			if p, ok := nullPointer(v, fmt.Sprintf("%s/%d", path, i)); ok {
				return p, true
			}
		}
	}
	return "", false
}
