package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// stubChecker returns a fixed readiness code.
type stubChecker struct{ code string }

func (s stubChecker) Check(context.Context) string { return s.code }

// TestHealthzOK exercises GET /healthz over a real HTTP server and asserts the
// documented contract: 200, JSON content type, {"status":"ok"}.
func TestHealthzOK(t *testing.T) {
	srv := httptest.NewServer(NewHandler(nil, Deps{}))
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/healthz")
	if err != nil {
		t.Fatalf("GET /healthz: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
	if got, want := resp.Header.Get("Content-Type"), "application/json; charset=utf-8"; got != want {
		t.Errorf("Content-Type = %q, want %q", got, want)
	}

	var body struct {
		Status string `json:"status"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("response body is not valid JSON: %v", err)
	}
	if body.Status != "ok" {
		t.Errorf("status field = %q, want %q", body.Status, "ok")
	}
}

// TestHealthzRejectsNonGET verifies the method-routed pattern only accepts GET.
func TestHealthzRejectsNonGET(t *testing.T) {
	srv := httptest.NewServer(NewHandler(nil, Deps{}))
	defer srv.Close()

	resp, err := http.Post(srv.URL+"/healthz", "application/json", nil)
	if err != nil {
		t.Fatalf("POST /healthz: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusMethodNotAllowed)
	}
}

// TestUnknownPathIs404 documents that unimplemented routes are absent rather
// than stubbed.
func TestUnknownPathIs404(t *testing.T) {
	srv := httptest.NewServer(NewHandler(nil, Deps{}))
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/v1/transactions/anything")
	if err != nil {
		t.Fatalf("GET unknown path: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusNotFound)
	}
}

// TestHealthzIsReadinessIndependent: liveness must stay truthful regardless of
// dependency state — even an explicitly unreachable database.
func TestHealthzIsReadinessIndependent(t *testing.T) {
	srv := httptest.NewServer(NewHandler(stubChecker{code: "unreachable"}, Deps{}))
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/healthz")
	if err != nil {
		t.Fatalf("GET /healthz: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
}

func getReadyz(t *testing.T, handler http.Handler) (int, map[string]string) {
	t.Helper()
	srv := httptest.NewServer(handler)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/readyz")
	if err != nil {
		t.Fatalf("GET /readyz: %v", err)
	}
	defer resp.Body.Close()

	var body struct {
		Status string            `json:"status"`
		Checks map[string]string `json:"checks"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("response body is not valid JSON: %v", err)
	}
	checks := body.Checks
	if checks == nil {
		checks = map[string]string{}
	}
	checks["__status"] = body.Status
	return resp.StatusCode, checks
}

// TestReadyzNotConfigured: no checker wired (no DATABASE_URL) -> 503.
func TestReadyzNotConfigured(t *testing.T) {
	code, checks := getReadyz(t, NewHandler(nil, Deps{}))
	if code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d", code, http.StatusServiceUnavailable)
	}
	if checks["database"] != "not_configured" {
		t.Fatalf("checks.database = %q, want not_configured", checks["database"])
	}
}

// TestReadyzOK: every dependency usable -> 200 {"status":"ok"}.
func TestReadyzOK(t *testing.T) {
	code, checks := getReadyz(t, NewHandler(stubChecker{code: "ok"}, Deps{}))
	if code != http.StatusOK {
		t.Fatalf("status = %d, want %d", code, http.StatusOK)
	}
	if checks["__status"] != "ok" {
		t.Fatalf("status field = %q, want ok", checks["__status"])
	}
	if _, present := checks["database"]; present {
		t.Fatalf("checks should be omitted when ready, got %v", checks)
	}
}

// TestReadyzSchemaNotReady: migrations missing -> 503 with the reason code.
func TestReadyzSchemaNotReady(t *testing.T) {
	code, checks := getReadyz(t, NewHandler(stubChecker{code: "schema_not_ready"}, Deps{}))
	if code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d", code, http.StatusServiceUnavailable)
	}
	if checks["database"] != "schema_not_ready" {
		t.Fatalf("checks.database = %q, want schema_not_ready", checks["database"])
	}
	if checks["__status"] != "unavailable" {
		t.Fatalf("status field = %q, want unavailable", checks["__status"])
	}
}

// TestReadyzDatabaseUnreachable: database down -> 503 with the reason code.
func TestReadyzDatabaseUnreachable(t *testing.T) {
	code, checks := getReadyz(t, NewHandler(stubChecker{code: "unreachable"}, Deps{}))
	if code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d", code, http.StatusServiceUnavailable)
	}
	if checks["database"] != "unreachable" {
		t.Fatalf("checks.database = %q, want unreachable", checks["database"])
	}
}
