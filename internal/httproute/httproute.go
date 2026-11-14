// Package httproute carries the route pattern a request matched back out to the middleware that
// wrapped the handler. A ServeMux sets Request.Pattern only on the request it dispatches, which is
// lost to a middleware outside it as soon as a layer in between clones the request.
package httproute

import (
	"context"
	"net/http"
)

type key struct{}

// Route is filled in by Note while the request is served.
type Route struct {
	pattern string
}

// Pattern is the last pattern noted, and empty when no mux matched the request.
func (r *Route) Pattern() string {
	return r.pattern
}

// Track returns a context whose Notes land in the returned Route.
func Track(ctx context.Context) (context.Context, *Route) {
	route := &Route{}
	return context.WithValue(ctx, key{}, route), route
}

// Note records the pattern mux matches for r, empty when it matches none. A no-op on a request
// nobody tracks.
func Note(mux *http.ServeMux, r *http.Request) {
	route, ok := r.Context().Value(key{}).(*Route)
	if !ok {
		return
	}
	_, route.pattern = mux.Handler(r)
}

// Noting wraps mux so every request it serves is noted first.
func Noting(mux *http.ServeMux) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		Note(mux, r)
		mux.ServeHTTP(w, r)
	})
}
