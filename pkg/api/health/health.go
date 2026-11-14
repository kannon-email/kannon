// Package health serves the readiness check on a listener of its own, meant for probes inside the
// cluster and kept out of the published REST API.
package health

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
)

// NewHandler answers GET /health with 200 when ready returns nil and 503 otherwise. A nil ready
// reports the service as unavailable. The response never carries the failure's detail.
func NewHandler(ready func(context.Context) error) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		status, body := http.StatusOK, "ready"
		if ready == nil || ready(r.Context()) != nil {
			status, body = http.StatusServiceUnavailable, "unavailable"
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(status)
		if err := json.NewEncoder(w).Encode(map[string]string{"status": body}); err != nil {
			slog.Debug("cannot write health response", "err", err)
		}
	})
	return mux
}
