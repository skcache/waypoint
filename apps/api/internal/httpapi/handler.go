// Package httpapi wires the Waypoint HTTP surface.
//
// Phase 01 exposes process liveness only. The /api/v1 routes from the product
// specification are added in the phases that implement their behavior.
package httpapi

import (
	"encoding/json"
	"log/slog"
	"net/http"
)

// NewHandler builds the root HTTP handler for the API.
func NewHandler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", handleHealthz)
	return mux
}

// healthResponse is the JSON body of GET /healthz.
type healthResponse struct {
	Status string `json:"status"`
}

// handleHealthz reports process liveness without touching external
// dependencies — liveness must stay truthful even when services are down.
// Database-aware readiness (/readyz) arrives with the database phase.
func handleHealthz(w http.ResponseWriter, _ *http.Request) {
	body, err := json.Marshal(healthResponse{Status: "ok"})
	if err != nil {
		// Unreachable with a plain string field; fail loudly rather than
		// serve a malformed body.
		slog.Error("encoding health response", "error", err)
		http.Error(w, `{"error":{"code":"INTERNAL_ERROR"}}`, http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	if _, err := w.Write(body); err != nil {
		slog.Error("writing health response", "error", err)
	}
}
