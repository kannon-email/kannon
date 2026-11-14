package rest

import (
	"context"
	"net/http"

	"github.com/kannon-email/kannon/pkg/api/httproute"
)

type apiVersion struct {
	prefix   string
	contract []byte
}

// versions share one implementation: v1beta1 is a deprecated alias of v1 with the same
// contract, kept until its removal.
var versions = []apiVersion{
	{prefix: "/v1", contract: contractV1},
	{prefix: "/v1beta1", contract: contractV1beta1},
}

type prefixKey struct{}

func mountVersions(routes http.Handler) http.Handler {
	root := http.NewServeMux()
	for _, v := range versions {
		root.HandleFunc("GET "+v.prefix+"/openapi.yaml", contractHandler(v.contract))
		scoped := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			routes.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), prefixKey{}, v.prefix)))
		})
		root.Handle(v.prefix+"/", http.StripPrefix(v.prefix, scoped))
	}
	return httproute.OverwritingMatch(root)
}

// versionedPath returns path under the API version the request was made to, so a client of a
// version is never pointed at another one.
func versionedPath(r *http.Request, path string) string {
	prefix, ok := r.Context().Value(prefixKey{}).(string)
	if !ok {
		return path
	}
	return prefix + path
}
