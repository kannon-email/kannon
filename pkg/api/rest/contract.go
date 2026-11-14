package rest

import (
	_ "embed"
	"log/slog"
	"net/http"
)

// contractV1 is the approved OpenAPI document, served as is so integrators and documentation
// sites read the contract of the running version.
//
//go:embed openapi.yaml
var contractV1 []byte

// contractV1beta1 is the v1 contract under the deprecated v1beta1 version.
//
//go:embed openapi_v1beta1.yaml
var contractV1beta1 []byte

// contractHandler publishes a contract without authentication. It is a static description of
// the API, not one of its operations, so the contract does not list it.
func contractHandler(contract []byte) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/yaml")
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Cache-Control", "public, max-age=300")
		if _, err := w.Write(contract); err != nil {
			slog.Debug("cannot write REST contract", "err", err)
		}
	}
}
