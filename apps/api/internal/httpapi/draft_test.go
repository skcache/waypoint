package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/skcache/waypoint/apps/api/internal/intent"
	"github.com/skcache/waypoint/apps/api/internal/llm"
)

// fakeCompiler is a TEST-ONLY deterministic stand-in for the intent
// compiler. Production wiring always uses intent.NewHostedCompiler over the
// real llm.Client (see cmd/server); a fake must never power a user-visible
// "AI" result (PRD §9.4).
type fakeCompiler struct {
	result     intent.Result
	err        error
	calls      int
	gotRequest string
}

func (f *fakeCompiler) Compile(_ context.Context, request string) (intent.Result, error) {
	f.calls++
	f.gotRequest = request
	return f.result, f.err
}

func draftResult() intent.Result {
	currency := "USD"
	ceiling := int64(15000)
	qty := 1
	category := "keyboard"
	return intent.Result{
		Draft: intent.Draft{
			Status:          intent.DraftStatusDraft,
			Spendable:       false,
			OriginalRequest: "Buy one quiet office keyboard, under $150.",
			Terms: intent.Terms{
				Currency:               &currency,
				MaxTotalMinor:          &ceiling,
				MaxQuantity:            &qty,
				Category:               &category,
				QualitativePreferences: []string{"quiet"},
			},
			UnresolvedFields: []string{},
			Assumptions:      []string{},
		},
		Provider: "api.test.example",
		Model:    "test-model",
	}
}

func draftServer(t *testing.T, deps Deps) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(NewHandler(nil, deps))
	t.Cleanup(srv.Close)
	return srv
}

func postDraft(t *testing.T, srv *httptest.Server, body string, headers map[string]string) *http.Response {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, srv.URL+"/api/v1/intents/draft", strings.NewReader(body))
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST draft: %v", err)
	}
	t.Cleanup(func() { _ = resp.Body.Close() })
	return resp
}

func decodeErrorEnvelope(t *testing.T, resp *http.Response) errorBody {
	t.Helper()
	var env struct {
		Error errorBody `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&env); err != nil {
		t.Fatalf("error envelope is not valid JSON: %v", err)
	}
	return env.Error
}

func TestDraftSuccessContract(t *testing.T) {
	fake := &fakeCompiler{result: draftResult()}
	srv := draftServer(t, Deps{IntentCompiler: fake})

	resp := postDraft(t, srv, `{"request":"  Buy one quiet office keyboard, under $150.  "}`, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if got := resp.Header.Get("Cache-Control"); got != "no-store" {
		t.Errorf("Cache-Control = %q, want no-store", got)
	}
	if fake.gotRequest != "Buy one quiet office keyboard, under $150." {
		t.Errorf("compiler received %q, want the trimmed request", fake.gotRequest)
	}

	var body struct {
		Draft struct {
			Status           string          `json:"status"`
			Spendable        bool            `json:"spendable"`
			OriginalRequest  string          `json:"original_request"`
			Terms            json.RawMessage `json:"terms"`
			UnresolvedFields []string        `json:"unresolved_fields"`
			Assumptions      []string        `json:"assumptions"`
		} `json:"draft"`
		Model struct {
			Provider string `json:"provider"`
			Name     string `json:"name"`
		} `json:"model"`
		RequestID string `json:"request_id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("success body is not valid JSON: %v", err)
	}
	if body.Draft.Status != "DRAFT" {
		t.Errorf("draft.status = %q, want DRAFT", body.Draft.Status)
	}
	if body.Draft.Spendable {
		t.Error("draft.spendable = true; a draft must never be spendable")
	}
	if body.Draft.UnresolvedFields == nil || body.Draft.Assumptions == nil {
		t.Error("list fields must serialize as [], not null")
	}
	if body.Model.Provider != "api.test.example" || body.Model.Name != "test-model" {
		t.Errorf("model = %+v", body.Model)
	}
	if body.RequestID == "" {
		t.Error("request_id is empty")
	}
}

func TestDraftNotConfigured(t *testing.T) {
	srv := draftServer(t, Deps{})
	resp := postDraft(t, srv, `{"request":"Buy a keyboard."}`, nil)
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", resp.StatusCode)
	}
	eb := decodeErrorEnvelope(t, resp)
	if eb.Code != "NOT_CONFIGURED" || eb.Retryable {
		t.Fatalf("error = %+v", eb)
	}
	if eb.TransactionID != nil {
		t.Errorf("transaction_id = %v, want null", eb.TransactionID)
	}
}

func TestDraftCompilerErrorMapping(t *testing.T) {
	cases := []struct {
		name      string
		err       error
		status    int
		code      string
		retryable bool
	}{
		{"invalid output", fmt.Errorf("%w: max_total_minor must ...", intent.ErrInvalidOutput), http.StatusBadGateway, "COMPILATION_FAILED", true},
		{"provider timeout", &llm.Error{Kind: llm.KindUnavailable, Detail: "the request timed out"}, http.StatusBadGateway, "PROVIDER_UNAVAILABLE", true},
		{"provider rejected", &llm.Error{Kind: llm.KindRejected, Status: 401, Detail: "the provider rejected the request"}, http.StatusBadGateway, "PROVIDER_UNAVAILABLE", false},
	}
	for _, tc := range cases {
		srv := draftServer(t, Deps{IntentCompiler: &fakeCompiler{err: tc.err}})
		resp := postDraft(t, srv, `{"request":"Buy a keyboard."}`, nil)
		if resp.StatusCode != tc.status {
			t.Errorf("%s: status = %d, want %d", tc.name, resp.StatusCode, tc.status)
		}
		eb := decodeErrorEnvelope(t, resp)
		if eb.Code != tc.code || eb.Retryable != tc.retryable {
			t.Errorf("%s: error = %+v, want code=%s retryable=%v", tc.name, eb, tc.code, tc.retryable)
		}
	}
}

func TestDraftInternalErrorIsSanitized(t *testing.T) {
	secret := "sk-live-do-not-leak-0123456789"
	srv := draftServer(t, Deps{IntentCompiler: &fakeCompiler{err: errors.New("boom " + secret)}})
	resp := postDraft(t, srv, `{"request":"Buy a keyboard."}`, nil)
	if resp.StatusCode != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", resp.StatusCode)
	}
	raw := make([]byte, 4096)
	n, _ := resp.Body.Read(raw)
	if strings.Contains(string(raw[:n]), secret) {
		t.Fatalf("response body leaks internal error text: %q", string(raw[:n]))
	}
}

func TestDraftRequestValidation(t *testing.T) {
	longRequest := `{"request":"` + strings.Repeat("a", intent.MaxRequestTextLen+1) + `"}`
	oversized := `{"request":"` + strings.Repeat("a", maxDraftBodyBytes) + `"}`
	cases := []struct {
		name        string
		body        string
		contentType string
		status      int
	}{
		{"not JSON", `hello`, "application/json", http.StatusBadRequest},
		{"unknown field", `{"request":"x","confirmed":true}`, "application/json", http.StatusBadRequest},
		{"trailing data", `{"request":"x"}{}`, "application/json", http.StatusBadRequest},
		{"missing request", `{}`, "application/json", http.StatusBadRequest},
		{"empty request", `{"request":"   "}`, "application/json", http.StatusBadRequest},
		{"too long", longRequest, "application/json", http.StatusBadRequest},
		{"control char", `{"request":"bad\u0007char"}`, "application/json", http.StatusBadRequest},
		{"wrong content type", `{"request":"x"}`, "text/plain", http.StatusUnsupportedMediaType},
		{"missing content type", `{"request":"x"}`, "", http.StatusUnsupportedMediaType},
		{"oversized body", oversized, "application/json", http.StatusRequestEntityTooLarge},
	}
	for _, tc := range cases {
		fake := &fakeCompiler{result: draftResult()}
		srv := draftServer(t, Deps{IntentCompiler: fake})
		headers := map[string]string{}
		if tc.contentType != "" {
			headers["Content-Type"] = tc.contentType
		} else {
			headers["Content-Type"] = "none" // sent, then removed below
		}
		resp := postDraft(t, srv, tc.body, headers)
		if tc.contentType == "" {
			// postDraft always sets a JSON content type; emulate absence by
			// deleting it on a fresh request instead.
			resp.Body.Close()
			req, _ := http.NewRequest(http.MethodPost, srv.URL+"/api/v1/intents/draft", strings.NewReader(tc.body))
			r2, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatalf("%s: %v", tc.name, err)
			}
			defer r2.Body.Close()
			resp = r2
		}
		if resp.StatusCode != tc.status {
			t.Errorf("%s: status = %d, want %d", tc.name, resp.StatusCode, tc.status)
		}
		eb := decodeErrorEnvelope(t, resp)
		if eb.Code != "VALIDATION_ERROR" {
			t.Errorf("%s: code = %q, want VALIDATION_ERROR", tc.name, eb.Code)
		}
		if fake.calls != 0 {
			t.Errorf("%s: compiler was called %d times for an invalid request", tc.name, fake.calls)
		}
	}
}

func TestDraftOriginPolicy(t *testing.T) {
	fake := func() *fakeCompiler { return &fakeCompiler{result: draftResult()} }

	srv := draftServer(t, Deps{IntentCompiler: fake(), AllowedOrigin: "http://localhost:3000"})
	if resp := postDraft(t, srv, `{"request":"Buy a keyboard."}`, map[string]string{"Origin": "http://localhost:3000"}); resp.StatusCode != http.StatusOK {
		t.Errorf("matching origin: status = %d, want 200", resp.StatusCode)
	}
	if resp := postDraft(t, srv, `{"request":"Buy a keyboard."}`, map[string]string{"Origin": "http://evil.example"}); resp.StatusCode != http.StatusForbidden {
		t.Errorf("foreign origin: status = %d, want 403", resp.StatusCode)
	} else if eb := decodeErrorEnvelope(t, resp); eb.Code != "UNAUTHORIZED" {
		t.Errorf("foreign origin: code = %q, want UNAUTHORIZED", eb.Code)
	}
	// Server-to-server calls carry no Origin header and stay allowed until
	// Phase 07 introduces demo sessions.
	if resp := postDraft(t, srv, `{"request":"Buy a keyboard."}`, nil); resp.StatusCode != http.StatusOK {
		t.Errorf("no origin: status = %d, want 200", resp.StatusCode)
	}
}

func TestDraftRejectsNonPOST(t *testing.T) {
	srv := draftServer(t, Deps{IntentCompiler: &fakeCompiler{result: draftResult()}})
	resp, err := http.Get(srv.URL + "/api/v1/intents/draft")
	if err != nil {
		t.Fatalf("GET draft: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405", resp.StatusCode)
	}
}

// TestDraftContractSuccessBodyIsStable pins the documented success shape: a
// draft that is explicitly non-authoritative, with model provenance and a
// correlation id.
func TestDraftContractSuccessBodyIsStable(t *testing.T) {
	srv := draftServer(t, Deps{IntentCompiler: &fakeCompiler{result: draftResult()}})
	resp := postDraft(t, srv, `{"request":"Buy one quiet office keyboard, under $150."}`, nil)

	var raw map[string]json.RawMessage
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		t.Fatalf("decode: %v", err)
	}
	for _, key := range []string{"draft", "model", "request_id"} {
		if _, ok := raw[key]; !ok {
			t.Errorf("success body is missing %q", key)
		}
	}
	var draft map[string]json.RawMessage
	if err := json.Unmarshal(raw["draft"], &draft); err != nil {
		t.Fatalf("decode draft: %v", err)
	}
	for _, key := range []string{"status", "spendable", "original_request", "terms", "unresolved_fields", "assumptions"} {
		if _, ok := draft[key]; !ok {
			t.Errorf("draft is missing %q", key)
		}
	}
	// The draft carries no confirmation or authority fields.
	for _, forbidden := range []string{"confirmed", "confirmed_at", "canonical_hash", "id", "version"} {
		if _, ok := draft[forbidden]; ok {
			t.Errorf("draft unexpectedly carries %q", forbidden)
		}
	}
}
