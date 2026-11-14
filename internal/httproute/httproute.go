// Package httproute carries the route pattern a request matched back out to the middleware that
// wrapped the handler. A ServeMux sets Request.Pattern only on the request it dispatches, which is
// lost to a middleware outside it as soon as a layer in between clones the request.
//
// Nested muxes each record their match in turn, outermost first, and each record overwrites the
// last: the pattern read back is the innermost mux's, the one that names the route.
package httproute

import (
	"context"
	"net/http"
)

type key struct{}

// MatchedRoute holds the pattern of the last mux that recorded a match for the request.
type MatchedRoute struct {
	pattern string
}

// Pattern is the pattern the last recording mux matched, and empty when it matched none or no mux
// recorded.
func (m *MatchedRoute) Pattern() string {
	return m.pattern
}

// WithMatchedRoute returns a context the muxes serving the request record their matches into, and
// the MatchedRoute to read the result from once they have.
func WithMatchedRoute(ctx context.Context) (context.Context, *MatchedRoute) {
	route := &MatchedRoute{}
	return context.WithValue(ctx, key{}, route), route
}

// OverwriteMatch records the pattern mux matches for r, empty when it matches none, replacing
// whatever an outer mux recorded. A no-op on a request with no MatchedRoute.
func OverwriteMatch(mux *http.ServeMux, r *http.Request) {
	route, ok := r.Context().Value(key{}).(*MatchedRoute)
	if !ok {
		return
	}
	_, route.pattern = mux.Handler(r)
}

// OverwritingMatch wraps mux so every request it serves records its match first.
func OverwritingMatch(mux *http.ServeMux) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		OverwriteMatch(mux, r)
		mux.ServeHTTP(w, r)
	})
}
