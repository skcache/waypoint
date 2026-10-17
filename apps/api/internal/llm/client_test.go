package llm

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

const testKey = "sk-test-SUPERSECRET-0123456789"

func testClient(t *testing.T, srv *httptest.Server) *Client {
	t.Helper()
	c, err := New(Config{BaseURL: srv.URL, APIKey: testKey, Model: "test-model"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return c
}

// TestGenerateSuccess pins the wire contract: POST to /chat/completions,
// bearer auth, system+user messages, temperature 0, bounded output tokens.
func TestGenerateSuccess(t *testing.T) {
	var gotAuth, gotCT string
	var gotBody chatRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotCT = r.Header.Get("Content-Type")
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Errorf("request body is not valid JSON: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"x","choices":[{"message":{"role":"assistant","content":"hello"},"finish_reason":"stop"}],"usage":{}}`))
	}))
	defer srv.Close()

	out, err := testClient(t, srv).Generate(context.Background(), "sys", "usr")
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if out != "hello" {
		t.Fatalf("out = %q, want %q", out, "hello")
	}
	if gotAuth != "Bearer "+testKey {
		t.Errorf("Authorization header = %q, want bearer with the configured key", gotAuth)
	}
	if gotCT != "application/json" {
		t.Errorf("Content-Type = %q", gotCT)
	}
	if gotBody.Model != "test-model" || len(gotBody.Messages) != 2 ||
		gotBody.Messages[0].Role != "system" || gotBody.Messages[0].Content != "sys" ||
		gotBody.Messages[1].Role != "user" || gotBody.Messages[1].Content != "usr" {
		t.Errorf("wire request = %+v", gotBody)
	}
	if gotBody.Temperature != 0 || gotBody.MaxTokens != maxOutputTokens {
		t.Errorf("temperature/max_tokens = %v/%d", gotBody.Temperature, gotBody.MaxTokens)
	}
}

func TestGeneratePreservesBasePath(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/chat/completions", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"ok"}}]}`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	c, err := New(Config{BaseURL: srv.URL + "/v1", APIKey: testKey, Model: "m"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, err := c.Generate(context.Background(), "s", "u"); err != nil {
		t.Fatalf("Generate: %v", err)
	}
}

// TestGenerateStatusClassification locks the rejection/unavailability split.
func TestGenerateStatusClassification(t *testing.T) {
	cases := []struct {
		status    int
		kind      ErrorKind
		retryable bool
	}{
		{400, KindRejected, false},
		{401, KindRejected, false},
		{403, KindRejected, false},
		{404, KindRejected, false},
		{422, KindRejected, false},
		{408, KindUnavailable, true},
		{429, KindUnavailable, true},
		{500, KindUnavailable, true},
		{503, KindUnavailable, true},
	}
	for _, tc := range cases {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(tc.status)
			_, _ = w.Write([]byte(`{"error":{"message":"provider-detail-` + testKey + `","type":"x"}}`))
		}))
		_, err := testClient(t, srv).Generate(context.Background(), "s", "u")
		srv.Close()

		var provErr *Error
		if !errors.As(err, &provErr) {
			t.Errorf("status %d: err = %v, want *Error", tc.status, err)
			continue
		}
		if provErr.Kind != tc.kind || provErr.Status != tc.status {
			t.Errorf("status %d: kind=%s retryable=%v, want kind=%s", tc.status, provErr.Kind, provErr.Retryable(), tc.kind)
		}
		if provErr.Retryable() != tc.retryable {
			t.Errorf("status %d: Retryable() = %v, want %v", tc.status, provErr.Retryable(), tc.retryable)
		}
		if strings.Contains(err.Error(), testKey) || strings.Contains(err.Error(), "provider-detail") {
			t.Errorf("status %d: error leaks provider body: %q", tc.status, err.Error())
		}
	}
}

func TestGenerateUnreadableResponses(t *testing.T) {
	bodies := []string{
		`{not json`,      // malformed envelope
		`{"choices":[]}`, // no choices
		`{"choices":[{"message":{"content":""}}]}`,    // empty content
		`{"choices":[{"message":{"content":"hi"}}]}x`, // trailing garbage
		`{"error":{"message":"nope","type":"x"}}`,     // provider error object on 2xx
	}
	for _, body := range bodies {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(body))
		}))
		_, err := testClient(t, srv).Generate(context.Background(), "s", "u")
		srv.Close()

		var provErr *Error
		if !errors.As(err, &provErr) || provErr.Kind != KindUnavailable {
			t.Errorf("body %q: err = %v, want unavailable *Error", body, err)
		}
	}
}

func TestGenerateTimeoutIsBoundedAndClassified(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(1500 * time.Millisecond)
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"late"}}]}`))
	}))
	defer srv.Close()

	c, err := newWithClient(
		Config{BaseURL: srv.URL, APIKey: testKey, Model: "m"},
		&http.Client{Timeout: 100 * time.Millisecond},
	)
	if err != nil {
		t.Fatalf("newWithClient: %v", err)
	}
	start := time.Now()
	_, err = c.Generate(context.Background(), "s", "u")
	elapsed := time.Since(start)

	var provErr *Error
	if !errors.As(err, &provErr) || provErr.Kind != KindUnavailable {
		t.Fatalf("err = %v, want unavailable *Error", err)
	}
	if elapsed > 2*time.Second {
		t.Fatalf("timeout not bounded: took %v", elapsed)
	}
}

func TestGenerateHonorsContextCancellation(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(500 * time.Millisecond)
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := testClient(t, srv).Generate(ctx, "s", "u")
	var provErr *Error
	if !errors.As(err, &provErr) || provErr.Kind != KindUnavailable {
		t.Fatalf("err = %v, want unavailable *Error", err)
	}
}

func TestRedirectPolicy(t *testing.T) {
	c, err := New(Config{BaseURL: "https://api.example.com", APIKey: testKey, Model: "m"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	mk := func(scheme, host string) (*http.Request, []*http.Request) {
		orig, _ := http.NewRequest(http.MethodPost, "https://api.example.com/chat/completions", nil)
		target, _ := http.NewRequest(http.MethodPost, scheme+"://"+host+"/next", nil)
		return target, []*http.Request{orig}
	}

	req, via := mk("https", "api.example.com")
	if err := c.checkRedirect(req, via); err != nil {
		t.Errorf("same-host https redirect refused: %v", err)
	}
	req, via = mk("https", "evil.example.com")
	if err := c.checkRedirect(req, via); err == nil {
		t.Error("cross-host redirect allowed")
	}
	req, via = mk("http", "api.example.com")
	if err := c.checkRedirect(req, via); err == nil {
		t.Error("scheme downgrade allowed")
	}
	req, via = mk("https", "api.example.com")
	if err := c.checkRedirect(req, append(via, via[0], via[0])); err == nil {
		t.Error("unbounded redirect chain allowed")
	}
}

// TestRedirectDowngradeEndToEnd drives the refusal through a real HTTPS
// server redirecting to plain http.
func TestRedirectDowngradeEndToEnd(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "http://127.0.0.1:9/next", http.StatusFound)
	}))
	defer srv.Close()

	c, err := newWithClient(
		Config{BaseURL: srv.URL, APIKey: testKey, Model: "m"},
		srv.Client(),
	)
	if err != nil {
		t.Fatalf("newWithClient: %v", err)
	}
	_, err = c.Generate(context.Background(), "s", "u")
	var provErr *Error
	if !errors.As(err, &provErr) || provErr.Kind != KindUnavailable {
		t.Fatalf("err = %v, want unavailable *Error", err)
	}
}

func TestValidateBaseURL(t *testing.T) {
	valid := []string{
		"https://api.example.com",
		"https://api.example.com/v1",
		"http://127.0.0.1:9999",
		"http://localhost:1234/v1",
		"http://[::1]:8080",
	}
	for _, raw := range valid {
		if err := ValidateBaseURL(raw); err != nil {
			t.Errorf("ValidateBaseURL(%q) = %v, want nil", raw, err)
		}
	}
	invalid := []string{
		"http://api.example.com",            // plain http off loopback
		"ftp://api.example.com",             // unsupported scheme
		"https://user:pass@api.example.com", // embedded credentials
		"https://api.example.com?k=v",       // query
		"https://api.example.com#frag",      // fragment
		"https://",                          // missing host
		"not a url",                         // no scheme
		"",
	}
	for _, raw := range invalid {
		err := ValidateBaseURL(raw)
		if err == nil {
			t.Errorf("ValidateBaseURL(%q) = nil, want error", raw)
			continue
		}
		if strings.Contains(err.Error(), "pass") || strings.Contains(err.Error(), "user:") {
			t.Errorf("ValidateBaseURL(%q) error leaks credentials: %q", raw, err.Error())
		}
	}
}

func TestConfigFromEnv(t *testing.T) {
	full := map[string]string{
		"LLM_API_BASE_URL": "https://api.example.com",
		"LLM_API_KEY":      testKey,
		"LLM_MODEL":        "model-x",
	}
	cfg, err := ConfigFromEnv(func(k string) string { return full[k] })
	if err != nil {
		t.Fatalf("ConfigFromEnv: %v", err)
	}
	if !cfg.Configured() {
		t.Error("Configured() = false for a complete configuration")
	}

	for _, missing := range []string{"LLM_API_BASE_URL", "LLM_API_KEY", "LLM_MODEL"} {
		env := map[string]string{}
		for k, v := range full {
			if k != missing {
				env[k] = v
			}
		}
		_, err := ConfigFromEnv(func(k string) string { return env[k] })
		if !errors.Is(err, ErrNotConfigured) {
			t.Errorf("missing %s: err = %v, want ErrNotConfigured", missing, err)
		}
	}

	env := map[string]string{
		"LLM_API_BASE_URL": "http://api.example.com", // non-loopback plain http
		"LLM_API_KEY":      testKey,
		"LLM_MODEL":        "model-x",
	}
	_, err = ConfigFromEnv(func(k string) string { return env[k] })
	if err == nil {
		t.Error("plain http off loopback accepted")
	}
	if strings.Contains(err.Error(), testKey) {
		t.Errorf("configuration error leaks the key: %q", err.Error())
	}
}
