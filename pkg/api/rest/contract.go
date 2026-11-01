package rest

import (
	_ "embed"
	"log/slog"
	"net/http"
)

// contract is the approved OpenAPI document, served as is so integrators and documentation
// sites read the contract of the running version.
//
//go:embed openapi.yaml
var contract []byte

// serveContract publishes the contract without authentication. It is a static description of
// the API, not one of its operations, so the contract does not list it.
func serveContract(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/yaml")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Cache-Control", "public, max-age=300")
	if _, err := w.Write(contract); err != nil {
		slog.Debug("cannot write REST contract", "err", err)
	}
}
