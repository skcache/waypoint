// Package httpapi wires the Waypoint HTTP surface.
//
// Implemented routes: GET /healthz (process liveness, dependency-free) and
// GET /readyz (dependency readiness). The /api/v1 routes from the product
// specification are added in the phases that implement their behavior.
package httpapi

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
)

// ReadinessChecker reports whether the API's required dependencies are
// usable. Check returns the literal string "ok" when ready; any other value
// is a short machine-readable reason code such as "not_configured",
// "unreachable" or "schema_not_ready". Implementations must never include
// connection details or credentials in the code.
type ReadinessChecker interface {
	Check(ctx context.Context) string
}

// NewHandler builds the root HTTP handler. A nil checker makes every
// readiness probe report "not_configured".
func NewHandler(ready ReadinessChecker) http.Handler {
	h := &handler{ready: ready}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", h.handleHealthz)
	mux.HandleFunc("GET /readyz", h.handleReadyz)
	return mux
}

type handler struct {
	ready ReadinessChecker
}

// healthResponse is the JSON body of GET /healthz.
type healthResponse struct {
	Status string `json:"status"`
}

// readyResponse is the JSON body of GET /readyz.
type readyResponse struct {
	Status string            `json:"status"`
	Checks map[string]string `json:"checks,omitempty"`
}

// handleHealthz reports process liveness without touching external
// dependencies — liveness must stay truthful even when services are down.
func (h *handler) handleHealthz(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, healthResponse{Status: "ok"}, "health")
}

// handleReadyz reports dependency readiness: 200 only when every required
// dependency is usable; 503 with a non-secret reason code otherwise.
func (h *handler) handleReadyz(w http.ResponseWriter, r *http.Request) {
	code := "not_configured"
	if h.ready != nil {
		code = h.ready.Check(r.Context())
	}

	if code == "ok" {
		writeJSON(w, http.StatusOK, readyResponse{Status: "ok"}, "readiness")
		return
	}
	writeJSON(w, http.StatusServiceUnavailable, readyResponse{
		Status: "unavailable",
		Checks: map[string]string{"database": code},
	}, "readiness")
}

// writeJSON encodes v as the response body. An encode failure is logged and
// answered with a minimal 500 rather than a partial body.
func writeJSON(w http.ResponseWriter, status int, v any, label string) {
	body, err := json.Marshal(v)
	if err != nil {
		slog.Error("encoding "+label+" response", "error", err)
		http.Error(w, `{"error":{"code":"INTERNAL_ERROR"}}`, http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if _, err := w.Write(body); err != nil {
		slog.Error("writing "+label+" response", "error", err)
	}
}
