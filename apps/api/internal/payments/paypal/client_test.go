package paypal

// Mock-server tests for the PayPal client. These run against a local
// httptest.Server standing in for the sandbox API — passing them is NOT
// evidence of live PayPal connectivity; see live_test.go for the separately
// gated live check.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

const (
	testClientID = "unit-test-client-id"
	testSecret   = "unit-test-client-secret-DO-NOT-ECHO"
	testToken    = "unit-access-token"
)

type recordedCall struct {
	Method      string
	Path        string
	Auth        string
	RequestID   string
	ContentType string
	Body        string
}

type fakePayPal struct {
	srv *httptest.Server

	tokenCalls atomic.Int32

	mu    sync.Mutex
	calls []recordedCall

	token   http.HandlerFunc
	create  http.HandlerFunc
	get     http.HandlerFunc
	capture http.HandlerFunc
}

func newFake(t *testing.T) (*fakePayPal, *Client) {
	t.Helper()
	f := &fakePayPal{}
	f.token = func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{
			"access_token": testToken, "token_type": "Bearer", "expires_in": 3600, "app_id": "APP-UNIT-TEST",
		})
	}
	f.create = func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusCreated, map[string]any{
			"id": "ORDERUNIT001", "status": "CREATED",
			"links": []map[string]string{
				{"href": "https://www.sandbox.paypal.com/checkoutnow?token=ORDERUNIT001", "rel": "payer-action", "method": "GET"},
				{"href": "https://api-m.sandbox.paypal.com/v2/checkout/orders/ORDERUNIT001", "rel": "self", "method": "GET"},
			},
		})
	}
	f.get = func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, orderJSON("ORDERUNIT001", "APPROVED", "1.00"))
	}
	f.capture = func(w http.ResponseWriter, r *http.Request) {
		body := orderJSON("ORDERUNIT001", "COMPLETED", "1.00")
		body["payer"] = map[string]any{"payer_id": "PAYERUNIT1"}
		body["purchase_units"] = []map[string]any{{
			"amount": map[string]any{"currency_code": "USD", "value": "1.00"},
			"payments": map[string]any{"captures": []map[string]any{{
				"id": "CAPTUREUNIT001", "status": "COMPLETED", "final_capture": true,
				"amount": map[string]any{"currency_code": "USD", "value": "1.00"},
			}}},
		}}
		writeJSON(w, http.StatusCreated, body)
	}
	f.srv = httptest.NewServer(http.HandlerFunc(f.route))
	t.Cleanup(f.srv.Close)
	client := newWithBase(
		Config{Environment: EnvironmentSandbox, ClientID: testClientID, ClientSecret: testSecret},
		f.srv.URL, f.srv.Client(),
	)
	return f, client
}

func (f *fakePayPal) route(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	f.mu.Lock()
	f.calls = append(f.calls, recordedCall{
		Method: r.Method, Path: r.URL.Path,
		Auth: r.Header.Get("Authorization"), RequestID: r.Header.Get("PayPal-Request-Id"),
		ContentType: r.Header.Get("Content-Type"),
		Body:        string(body),
	})
	f.mu.Unlock()

	switch {
	case r.URL.Path == tokenPath:
		f.tokenCalls.Add(1)
		f.token(w, r)
	case r.URL.Path == ordersPath && r.Method == http.MethodPost:
		f.create(w, r)
	case strings.HasSuffix(r.URL.Path, "/capture"):
		f.capture(w, r)
	case strings.HasPrefix(r.URL.Path, ordersPath+"/"):
		f.get(w, r)
	default:
		http.NotFound(w, r)
	}
}

func (f *fakePayPal) callsTo(path string) []recordedCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []recordedCall
	for _, c := range f.calls {
		if c.Path == path {
			out = append(out, c)
		}
	}
	return out
}

func (f *fakePayPal) totalCalls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.calls)
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func orderJSON(id, status, value string) map[string]any {
	return map[string]any{
		"id": id, "status": status,
		"purchase_units": []map[string]any{{
			"amount": map[string]any{"currency_code": "USD", "value": value},
		}},
	}
}

// slowHandler delays until the request context ends or the delay elapses, so
// timed-out tests never make httptest.Server.Close block.
func slowHandler(delay time.Duration, then http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
			return
		case <-time.After(delay):
		}
		then(w, r)
	}
}

func envMap(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func assertNoSecrets(t *testing.T, s string) {
	t.Helper()
	for _, banned := range []string{testSecret, testClientID, testToken, "Basic "} {
		if strings.Contains(s, banned) {
			t.Fatalf("error text leaks %q: %s", banned, s)
		}
	}
}

func TestConfigNotConfigured(t *testing.T) {
	cases := []struct {
		name string
		env  map[string]string
	}{
		{"nothing set", map[string]string{}},
		{"environment only", map[string]string{"PAYPAL_ENV": "sandbox"}},
		{"missing secret", map[string]string{"PAYPAL_ENV": "sandbox", "PAYPAL_CLIENT_ID": "x"}},
		{"missing client id", map[string]string{"PAYPAL_ENV": "sandbox", "PAYPAL_CLIENT_SECRET": "y"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ConfigFromEnv(envMap(tc.env))
			if !errors.Is(err, ErrNotConfigured) {
				t.Fatalf("err = %v, want ErrNotConfigured", err)
			}
		})
	}
}

func TestConfigRejectsNonSandboxEnvironments(t *testing.T) {
	for _, env := range []string{"production", "live", "Sandbox", "sandbox "} {
		_, err := ConfigFromEnv(envMap(map[string]string{
			"PAYPAL_ENV": env, "PAYPAL_CLIENT_ID": "x", "PAYPAL_CLIENT_SECRET": "y",
		}))
		if !errors.Is(err, ErrUnsupportedEnvironment) {
			t.Errorf("PAYPAL_ENV=%q: err = %v, want ErrUnsupportedEnvironment", env, err)
		}
	}
}

func TestClientIsSandboxLocked(t *testing.T) {
	c := New(Config{Environment: EnvironmentSandbox, ClientID: "x", ClientSecret: "y"})
	if c.BaseURL() != SandboxBaseURL {
		t.Fatalf("base URL = %q, want the sandbox constant %q", c.BaseURL(), SandboxBaseURL)
	}
	if !strings.Contains(SandboxBaseURL, "sandbox") {
		t.Fatalf("sandbox base URL constant looks wrong: %q", SandboxBaseURL)
	}
}

func TestAuthSuccessAndTokenCaching(t *testing.T) {
	f, c := newFake(t)
	ctx := context.Background()

	info, err := c.Authenticate(ctx)
	if err != nil {
		t.Fatalf("Authenticate: %v", err)
	}
	if info.AppID != "APP-UNIT-TEST" || info.ExpiresIn != 3600 || info.Reused {
		t.Fatalf("AuthInfo = %+v", info)
	}
	if got := f.tokenCalls.Load(); got != 1 {
		t.Fatalf("token calls = %d, want 1", got)
	}

	info2, err := c.Authenticate(ctx)
	if err != nil || !info2.Reused {
		t.Fatalf("second Authenticate = %+v, %v; want cached reuse", info2, err)
	}
	if got := f.tokenCalls.Load(); got != 1 {
		t.Fatalf("token calls = %d after reuse, want 1", got)
	}

	if _, err := c.CreateOrder(ctx, CreateOrderInput{
		Intent: "CAPTURE", CurrencyCode: "USD", AmountValue: "1.00", Description: SpikeDescription,
	}, "req-create-1"); err != nil {
		t.Fatalf("CreateOrder: %v", err)
	}
	if got := f.tokenCalls.Load(); got != 1 {
		t.Fatalf("token calls = %d; API calls must ride the cached token", got)
	}

	tokenCall := f.callsTo(tokenPath)
	if len(tokenCall) != 1 || !strings.HasPrefix(tokenCall[0].Auth, "Basic ") {
		t.Fatalf("token request auth = %+v, want HTTP Basic", tokenCall)
	}
	createCalls := f.callsTo(ordersPath)
	if len(createCalls) != 1 || createCalls[0].Auth != "Bearer "+testToken {
		t.Fatalf("create request auth = %+v, want bearer", createCalls)
	}
}

func TestAuthRejected(t *testing.T) {
	f, c := newFake(t)
	f.token = func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusUnauthorized, map[string]any{
			"name": "invalid_client", "message": "Client Authentication failed", "debug_id": "dbg-401",
		})
	}
	_, err := c.Authenticate(context.Background())
	if err == nil {
		t.Fatal("Authenticate succeeded with rejected credentials")
	}
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("err = %v, want *APIError", err)
	}
	if apiErr.StatusCode != 401 || apiErr.Name != "invalid_client" || apiErr.DebugID != "dbg-401" {
		t.Fatalf("APIError = %+v", apiErr)
	}
	assertNoSecrets(t, err.Error())
}

func TestAuthTimeout(t *testing.T) {
	f, c := newFake(t)
	f.token = slowHandler(2*time.Second, f.token)
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err := c.Authenticate(ctx)
	if err == nil {
		t.Fatal("Authenticate succeeded despite timeout")
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("auth took %s against a 100ms deadline", elapsed)
	}
	assertNoSecrets(t, err.Error())
}

func TestMalformedProviderJSON(t *testing.T) {
	t.Run("token body", func(t *testing.T) {
		f, c := newFake(t)
		f.token = func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte("this is not json")) }
		_, err := c.Authenticate(context.Background())
		if err == nil || !strings.Contains(err.Error(), "malformed response JSON") {
			t.Fatalf("err = %v, want malformed-response error", err)
		}
		assertNoSecrets(t, err.Error())
	})
	t.Run("create body", func(t *testing.T) {
		f, c := newFake(t)
		f.create = func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte("{broken")) }
		_, err := c.CreateOrder(context.Background(), CreateOrderInput{
			Intent: "CAPTURE", CurrencyCode: "USD", AmountValue: "1.00",
		}, "req-1")
		if err == nil || !strings.Contains(err.Error(), "malformed response JSON") {
			t.Fatalf("err = %v, want malformed-response error", err)
		}
	})
	t.Run("trailing garbage", func(t *testing.T) {
		f, c := newFake(t)
		f.token = func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(`{"access_token":"x","token_type":"Bearer","expires_in":3600} {"more":true}`))
		}
		_, err := c.Authenticate(context.Background())
		if err == nil || !strings.Contains(err.Error(), "trailing") {
			t.Fatalf("err = %v, want trailing-data error", err)
		}
	})
}

func TestCreateOrderSuccess(t *testing.T) {
	f, c := newFake(t)
	order, err := c.CreateOrder(context.Background(), CreateOrderInput{
		Intent: "CAPTURE", CurrencyCode: "USD", AmountValue: "1.00", Description: SpikeDescription,
	}, "req-create-1")
	if err != nil {
		t.Fatalf("CreateOrder: %v", err)
	}
	if order.ID != "ORDERUNIT001" || order.Status != "CREATED" {
		t.Fatalf("order = %+v", order)
	}
	if got := order.ApprovalURL(); !strings.Contains(got, "checkoutnow") {
		t.Fatalf("approval URL = %q", got)
	}

	calls := f.callsTo(ordersPath)
	if len(calls) != 1 {
		t.Fatalf("create calls = %d, want 1", len(calls))
	}
	if calls[0].RequestID != "req-create-1" {
		t.Fatalf("PayPal-Request-Id = %q", calls[0].RequestID)
	}
	for _, want := range []string{`"intent":"CAPTURE"`, `"currency_code":"USD"`, `"value":"1.00"`, SpikeDescription} {
		if !strings.Contains(calls[0].Body, want) {
			t.Fatalf("request body missing %s: %s", want, calls[0].Body)
		}
	}

	// A create without a request id must be refused before any network call.
	before := f.totalCalls()
	if _, err := c.CreateOrder(context.Background(), CreateOrderInput{
		Intent: "CAPTURE", CurrencyCode: "USD", AmountValue: "1.00",
	}, ""); err == nil {
		t.Fatal("CreateOrder without PayPal-Request-Id succeeded")
	}
	if f.totalCalls() != before {
		t.Fatal("refused create still reached the network")
	}
}

func TestCreateOrderRejected(t *testing.T) {
	f, c := newFake(t)
	f.create = func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]any{
			"name": "UNPROCESSABLE_ENTITY", "message": "The requested action could not be performed",
			"debug_id": "dbg-422",
			"details":  []map[string]any{{"issue": "INVALID_PARAMETER_SYNTAX"}},
		})
	}
	_, err := c.CreateOrder(context.Background(), CreateOrderInput{
		Intent: "CAPTURE", CurrencyCode: "USD", AmountValue: "1.00",
	}, "req-1")
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("err = %v, want *APIError", err)
	}
	if apiErr.StatusCode != 422 || apiErr.Name != "UNPROCESSABLE_ENTITY" || apiErr.DebugID != "dbg-422" || len(apiErr.Issues) != 1 {
		t.Fatalf("APIError = %+v", apiErr)
	}
	if AmbiguousOutcome(err) {
		t.Fatal("a 422 rejection must not be classified as ambiguous")
	}
	assertNoSecrets(t, err.Error())
}

func TestCreateOrderMissingOrderIDIsAmbiguous(t *testing.T) {
	f, c := newFake(t)
	f.create = func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusCreated, map[string]any{"status": "CREATED"})
	}
	_, err := c.CreateOrder(context.Background(), CreateOrderInput{
		Intent: "CAPTURE", CurrencyCode: "USD", AmountValue: "1.00",
	}, "req-1")
	var respErr *ResponseError
	if !errors.As(err, &respErr) {
		t.Fatalf("err = %v, want *ResponseError", err)
	}
	if !AmbiguousOutcome(err) {
		t.Fatal("a 2xx whose body lacks the order id cannot prove nothing happened")
	}
}

func TestRedirectPolicyRefusesDowngradeAndCrossHost(t *testing.T) {
	// A redirected request must never leave HTTPS or the host. The token
	// call is the first request the client makes, so the refusal surfaces
	// there.
	downgrade := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "http://downgrade.example.com/steal", http.StatusFound)
	}))
	t.Cleanup(downgrade.Close)
	client := newWithBase(
		Config{Environment: EnvironmentSandbox, ClientID: testClientID, ClientSecret: testSecret},
		downgrade.URL, defaultHTTPClient(),
	)
	_, err := client.Authenticate(context.Background())
	if err == nil || !strings.Contains(err.Error(), "refusing redirect") {
		t.Fatalf("err = %v, want an HTTPS-downgrade refusal", err)
	}

	crossHost := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "https://evil.example.com/steal", http.StatusFound)
	}))
	t.Cleanup(crossHost.Close)
	client2 := newWithBase(
		Config{Environment: EnvironmentSandbox, ClientID: testClientID, ClientSecret: testSecret},
		crossHost.URL, defaultHTTPClient(),
	)
	_, err = client2.Authenticate(context.Background())
	if err == nil || !strings.Contains(err.Error(), "refusing cross-host redirect") {
		t.Fatalf("err = %v, want a cross-host refusal", err)
	}
}

func TestOrderAndRequestIDValidationHappensBeforeNetwork(t *testing.T) {
	f, c := newFake(t)
	ctx := context.Background()
	for _, id := range []string{"", "lowercase", "has space", "BAD!CHAR", strings.Repeat("A", 37)} {
		if _, err := c.GetOrder(ctx, id); err == nil {
			t.Errorf("GetOrder(%q) succeeded, want validation error", id)
		}
	}
	if _, err := c.CaptureOrder(ctx, "ORDERUNIT001", ""); err == nil {
		t.Error("CaptureOrder with empty request id succeeded")
	}
	if _, err := c.CaptureOrder(ctx, "", "req-1"); err == nil {
		t.Error("CaptureOrder with empty order id succeeded")
	}
	if n := f.totalCalls(); n != 0 {
		t.Fatalf("validation failures reached the network: %d calls", n)
	}
}

func TestApprovalURLRels(t *testing.T) {
	both := &Order{Links: []Link{{Rel: "approve", Href: "https://a"}, {Rel: "payer-action", Href: "https://b"}}}
	if got := both.ApprovalURL(); got != "https://b" {
		t.Fatalf("payer-action must win, got %q", got)
	}
	old := &Order{Links: []Link{{Rel: "approve", Href: "https://a"}}}
	if got := old.ApprovalURL(); got != "https://a" {
		t.Fatalf("approve fallback failed, got %q", got)
	}
	if got := (&Order{}).ApprovalURL(); got != "" {
		t.Fatalf("no links must mean empty URL, got %q", got)
	}
}

func TestAmbiguousOutcomeClassification(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"local validation", newValidationError("bad id"), false},
		{"400", &APIError{StatusCode: 400}, false},
		{"401", &APIError{StatusCode: 401}, false},
		{"404", &APIError{StatusCode: 404}, false},
		{"415", &APIError{StatusCode: 415}, false},
		{"422", &APIError{StatusCode: 422}, false},
		{"408 request timeout", &APIError{StatusCode: 408}, true},
		{"429 rate limited", &APIError{StatusCode: 429}, true},
		{"unlisted 4xx", &APIError{StatusCode: 425}, true},
		{"500", &APIError{StatusCode: 500}, true},
		{"503", &APIError{StatusCode: 503}, true},
		{"response error", newResponseError("missing order id"), true},
		{"wrapped response error", fmt.Errorf("wrapped: %w", newResponseError("x")), true},
		{"wrapped 502", fmt.Errorf("wrapped: %w", &APIError{StatusCode: 502}), true},
		{"http timeout", &url.Error{Op: "Post", URL: "https://x.test/", Err: context.DeadlineExceeded}, true},
		{"connection break", &url.Error{Op: "Post", URL: "https://x.test/", Err: io.ErrUnexpectedEOF}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := AmbiguousOutcome(tc.err); got != tc.want {
				t.Fatalf("AmbiguousOutcome(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}

func TestErrorsNeverContainSecrets(t *testing.T) {
	ctx := context.Background()
	var errs []error

	f1, c1 := newFake(t)
	f1.token = func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusUnauthorized, map[string]any{"name": "invalid_client", "message": "Client Authentication failed"})
	}
	_, e := c1.Authenticate(ctx)
	errs = append(errs, e)

	f2, c2 := newFake(t)
	f2.create = func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]any{"name": "UNPROCESSABLE_ENTITY"})
	}
	_, e = c2.CreateOrder(ctx, CreateOrderInput{Intent: "CAPTURE", CurrencyCode: "USD", AmountValue: "1.00"}, "r")
	errs = append(errs, e)

	f3, c3 := newFake(t)
	f3.capture = func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"name": "INTERNAL_SERVER_ERROR"})
	}
	_, e = c3.CaptureOrder(ctx, "ORDERUNIT001", "r")
	errs = append(errs, e)

	f4, c4 := newFake(t)
	f4.token = slowHandler(2*time.Second, f4.token)
	tctx, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
	defer cancel()
	_, e = c4.Authenticate(tctx)
	errs = append(errs, e)

	f5, c5 := newFake(t)
	f5.srv.Close()
	_, e = c5.Authenticate(ctx)
	errs = append(errs, e)

	for i, err := range errs {
		if err == nil {
			t.Fatalf("scenario %d produced no error", i)
		}
		assertNoSecrets(t, err.Error())
	}
}
