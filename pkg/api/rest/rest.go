// Package rest implements the versioned HTTP adapter. Application modules own authorization.
package rest

//go:generate go run ./generate

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
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

// Services are the application modules the REST adapter exposes; they are shared with other
// transports and no HTTP types escape this package. Domain operations and readiness are always
// mounted; every other nil member leaves its routes unmounted. Ready must check the configured
// infrastructure, and a nil Ready reports the service as unavailable.
type Services struct {
	Domains   *domains.Service
	Token     admintoken.Token
	Ready     func(context.Context) error
	Keys      *apikeys.Service
	Templates *templates.Service
	Inline    *intake.InlineService
	Stats     *stats.Service
}

// NewHandler returns the versioned REST handler for the supplied services.
func NewHandler(s Services) http.Handler {
	service, token, ready, keys := s.Domains, s.Token, s.Ready, s.Keys
	events, content, inline := s.Stats, s.Templates, s.Inline
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
		if _, err := parseQuery(r.URL.RawQuery); err != nil {
			return err
		}
		input, err := decodeBody[DomainCreate](r.Body)
		if err != nil {
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
		q, err := parseQuery(r.URL.RawQuery, limitParameter, cursorParameter)
		if err != nil {
			return err
		}
		after, limit, err := parseDomainPage(q)
		if err != nil {
			return err
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
		if _, err := parseQuery(r.URL.RawQuery); err != nil {
			return err
		}
		name, err := parseDomainPath(r)
		if err != nil {
			return err
		}
		d, err := service.GetDomain(r.Context(), name)
		if err != nil {
			return err
		}
		writeJSON(w, 200, domainWire(d))
		return nil
	}))
	protected.Handle("PATCH /v1beta1/domains/{domain}", handler(func(w http.ResponseWriter, r *http.Request) error {
		if _, err := parseQuery(r.URL.RawQuery); err != nil {
			return err
		}
		name, err := parseDomainPath(r)
		if err != nil {
			return err
		}
		input, err := decodeBody[DomainPatch](r.Body)
		if err != nil {
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
			if err != nil && !errors.Is(err, apikeys.ErrKeyNotFound) {
				return err
			}
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

func parseDomainPage(q url.Values) (string, int, error) {
	limit, err := pageLimit(q)
	if err != nil {
		return "", 0, err
	}
	after := ""
	if q.Has(cursorParameter) {
		b, err := base64.RawURLEncoding.DecodeString(q.Get(cursorParameter))
		prefix := "v1beta1:domains:name:"
		if err != nil || !strings.HasPrefix(string(b), prefix) {
			return "", 0, problemError(400, "invalid_cursor")
		}
		after = strings.TrimPrefix(string(b), prefix)
		if _, err = values.Parse(after); err != nil {
			return "", 0, problemError(400, "invalid_cursor")
		}
	}
	return after, limit, nil
}
