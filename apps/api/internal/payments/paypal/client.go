package paypal

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

const (
	tokenPath  = "/v1/oauth2/token"
	ordersPath = "/v2/checkout/orders"

	// maxRequestIDLen keeps PayPal-Request-Id values far inside the current
	// API limit (10000 bytes) and the idempotency guidance (UUIDs, 38 single-
	// byte chars recommended).
	maxRequestIDLen = 100
)

// AuthInfo is the sanitized result of an authentication check. The access
// token itself is deliberately not exposed.
type AuthInfo struct {
	// AppID is the provider application id ("APP-…"), useful evidence that
	// the credentials are real. Not secret.
	AppID string
	// ExpiresIn is the provider-declared token lifetime in seconds.
	ExpiresIn int
	// Reused reports whether a cached token satisfied the request.
	Reused bool
}

// Client is a sandbox-only PayPal REST client.
type Client struct {
	cfg     Config
	baseURL string
	http    *http.Client

	mu        sync.Mutex
	token     string
	tokenExpr time.Time
}

// New returns a client locked to the PayPal sandbox base URL.
func New(cfg Config) *Client {
	return newWithBase(cfg, SandboxBaseURL, defaultHTTPClient())
}

// newWithBase exists so tests can point the client at a local fake server.
// Production code has no way to pass a base URL.
func newWithBase(cfg Config, baseURL string, hc *http.Client) *Client {
	if hc == nil {
		hc = defaultHTTPClient()
	}
	return &Client{cfg: cfg, baseURL: strings.TrimRight(baseURL, "/"), http: hc}
}

// BaseURL returns the endpoint this client talks to (evidence, tests).
func (c *Client) BaseURL() string { return c.baseURL }

func defaultHTTPClient() *http.Client {
	return &http.Client{
		Timeout: 30 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) == 0 {
				return nil
			}
			if req.URL.Host != via[0].URL.Host {
				return errors.New("paypal: refusing cross-host redirect")
			}
			if len(via) >= 3 {
				return errors.New("paypal: too many redirects")
			}
			return nil
		},
		// TLS defaults are fine; listed here so the transport intent is explicit.
		Transport: &http.Transport{
			TLSClientConfig:     &tls.Config{MinVersion: tls.VersionTLS12},
			MaxIdleConnsPerHost: 4,
		},
	}
}

// Authenticate obtains (or reuses) a sandbox access token and confirms that
// the credentials produce a valid OAuth response. The token is never returned.
func (c *Client) Authenticate(ctx context.Context) (AuthInfo, error) {
	tok, reused, err := c.ensureToken(ctx)
	if err != nil {
		return AuthInfo{}, err
	}
	return AuthInfo{AppID: tok.AppID, ExpiresIn: tok.ExpiresIn, Reused: reused}, nil
}

type tokenResponse struct {
	AccessToken string `json:"access_token"`
	TokenType   string `json:"token_type"`
	ExpiresIn   int    `json:"expires_in"`
	AppID       string `json:"app_id"`
}

func (c *Client) ensureToken(ctx context.Context) (tokenResponse, bool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.token != "" && time.Now().Before(c.tokenExpr.Add(-tokenSafetyMargin)) {
		return tokenResponse{AccessToken: c.token, ExpiresIn: int(time.Until(c.tokenExpr).Seconds())}, true, nil
	}

	form := "grant_type=client_credentials"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+tokenPath, strings.NewReader(form))
	if err != nil {
		return tokenResponse{}, false, fmt.Errorf("paypal: build token request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.SetBasicAuth(c.cfg.ClientID, c.cfg.ClientSecret)

	resp, err := c.http.Do(req)
	if err != nil {
		// Transport errors carry the method and URL only — never headers.
		return tokenResponse{}, false, fmt.Errorf("paypal: token request failed: %w", err)
	}
	defer func() {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
		_ = resp.Body.Close()
	}()

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return tokenResponse{}, false, parseAPIError(resp)
	}

	var tok tokenResponse
	if err := decodeStrict(resp.Body, &tok); err != nil {
		return tokenResponse{}, false, err
	}
	if tok.AccessToken == "" {
		return tokenResponse{}, false, newValidationError("token response carried no access token")
	}
	if !strings.EqualFold(tok.TokenType, "Bearer") {
		return tokenResponse{}, false, newValidationError("token response carried unexpected token type %q", tok.TokenType)
	}
	lifetime := tok.ExpiresIn
	if lifetime <= 0 {
		lifetime = 300 // defensive default; provider normally sends ~32400
	}
	c.token = tok.AccessToken
	c.tokenExpr = time.Now().Add(time.Duration(lifetime) * time.Second)
	return tok, false, nil
}

func (c *Client) invalidateToken() {
	c.mu.Lock()
	c.token = ""
	c.tokenExpr = time.Time{}
	c.mu.Unlock()
}

// call performs one authenticated API request. It retries exactly once on
// 401 with a fresh token; POST operations carry a stable PayPal-Request-Id,
// so the retry cannot duplicate a side effect.
func (c *Client) call(ctx context.Context, method, path, requestID string, in, out any) error {
	var payload []byte
	if in != nil {
		b, err := json.Marshal(in)
		if err != nil {
			return newValidationError("marshal request: %v", err)
		}
		payload = b
	}
	// PayPal answers 415 UNSUPPORTED_MEDIA_TYPE to a POST that carries no
	// parseable JSON payload at all (observed live on sandbox capture), so
	// empty POST bodies become an explicit empty JSON object.
	if payload == nil && method == http.MethodPost {
		payload = []byte("{}")
	}

	for attempt := 0; attempt < 2; attempt++ {
		tok, _, err := c.ensureToken(ctx)
		if err != nil {
			return err
		}
		var body io.Reader
		if payload != nil {
			body = bytes.NewReader(payload)
		}
		req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, body)
		if err != nil {
			return fmt.Errorf("paypal: build request: %w", err)
		}
		if payload != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		req.Header.Set("Authorization", "Bearer "+tok.AccessToken)
		if requestID != "" {
			req.Header.Set("PayPal-Request-Id", requestID)
		}

		resp, err := c.http.Do(req)
		if err != nil {
			return fmt.Errorf("paypal: request failed: %w", err)
		}
		if resp.StatusCode == http.StatusUnauthorized && attempt == 0 {
			_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
			_ = resp.Body.Close()
			c.invalidateToken()
			continue
		}
		return handleResponse(resp, out)
	}
	return newValidationError("authentication kept failing")
}

func handleResponse(resp *http.Response, out any) error {
	defer func() {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
		_ = resp.Body.Close()
	}()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return parseAPIError(resp)
	}
	if out == nil {
		return nil
	}
	return decodeStrict(resp.Body, out)
}

// decodeStrict decodes exactly one JSON document into out and rejects
// trailing garbage. Unknown fields are tolerated deliberately: PayPal adds
// response fields over time, and refusing them would break on provider
// evolution — required fields are validated explicitly after decoding.
func decodeStrict(r io.Reader, out any) error {
	dec := json.NewDecoder(io.LimitReader(r, maxResponseBytes))
	if err := dec.Decode(out); err != nil {
		return fmt.Errorf("paypal: malformed response JSON: %w", err)
	}
	if _, err := dec.Token(); err != io.EOF {
		return errors.New("paypal: malformed response JSON: unexpected trailing data")
	}
	return nil
}

type errorPayload struct {
	Name    string `json:"name"`
	Message string `json:"message"`
	DebugID string `json:"debug_id"`
	Details []struct {
		Issue       string `json:"issue"`
		Description string `json:"description"`
	} `json:"details"`
}

// parseAPIError builds a sanitized error from a provider error response. The
// raw body is never embedded: only the documented diagnostic fields survive.
func parseAPIError(resp *http.Response) error {
	ae := &APIError{StatusCode: resp.StatusCode}
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	var payload errorPayload
	if err := json.Unmarshal(raw, &payload); err == nil {
		ae.Name = payload.Name
		ae.Message = payload.Message
		ae.DebugID = payload.DebugID
		for _, d := range payload.Details {
			if d.Issue != "" {
				ae.Issues = append(ae.Issues, d.Issue)
			}
		}
	}
	if ae.Name == "" && ae.Message == "" {
		ae.Message = "unreadable provider error body"
	}
	return ae
}

// CreateOrderInput describes the synthetic purchase. Currency and amount are
// explicit strings because that is the provider contract; callers derive them
// from fixed constants, never from user input.
type CreateOrderInput struct {
	Intent       string
	CurrencyCode string
	AmountValue  string
	Description  string
}

type createOrderRequest struct {
	Intent        string               `json:"intent"`
	PurchaseUnits []purchaseUnitSource `json:"purchase_units"`
}

type purchaseUnitSource struct {
	Amount      moneySource `json:"amount"`
	Description string      `json:"description,omitempty"`
}

type moneySource struct {
	CurrencyCode string `json:"currency_code"`
	Value        string `json:"value"`
}

// CreateOrder creates a sandbox order. requestID is mandatory: the provider
// stores it and returns the latest result of a repeated request id instead of
// creating a duplicate order.
func (c *Client) CreateOrder(ctx context.Context, in CreateOrderInput, requestID string) (*Order, error) {
	if err := validateRequestID(requestID); err != nil {
		return nil, err
	}
	if in.Intent != "CAPTURE" && in.Intent != "AUTHORIZE" {
		return nil, newValidationError("unsupported intent %q", in.Intent)
	}
	if in.CurrencyCode == "" || in.AmountValue == "" {
		return nil, newValidationError("currency and amount are required")
	}
	req := createOrderRequest{
		Intent: in.Intent,
		PurchaseUnits: []purchaseUnitSource{{
			Amount:      moneySource{CurrencyCode: in.CurrencyCode, Value: in.AmountValue},
			Description: in.Description,
		}},
	}
	var out Order
	if err := c.call(ctx, http.MethodPost, ordersPath, requestID, req, &out); err != nil {
		return nil, err
	}
	if err := validateOrderResponse(&out); err != nil {
		return nil, err
	}
	return &out, nil
}

// GetOrder retrieves an order's current provider state.
func (c *Client) GetOrder(ctx context.Context, orderID string) (*Order, error) {
	if err := validateOrderID(orderID); err != nil {
		return nil, err
	}
	var out Order
	if err := c.call(ctx, http.MethodGet, ordersPath+"/"+orderID, "", nil, &out); err != nil {
		return nil, err
	}
	if err := validateOrderResponse(&out); err != nil {
		return nil, err
	}
	return &out, nil
}

// CaptureOrder captures an approved order. It sends a distinct, stable
// PayPal-Request-Id so that a retry after an unclear outcome returns the
// original result instead of capturing twice.
func (c *Client) CaptureOrder(ctx context.Context, orderID, requestID string) (*Order, error) {
	if err := validateOrderID(orderID); err != nil {
		return nil, err
	}
	if err := validateRequestID(requestID); err != nil {
		return nil, err
	}
	var out Order
	if err := c.call(ctx, http.MethodPost, ordersPath+"/"+orderID+"/capture", requestID, nil, &out); err != nil {
		return nil, err
	}
	if err := validateOrderResponse(&out); err != nil {
		return nil, err
	}
	return &out, nil
}

func validateOrderID(id string) error {
	if id == "" {
		return newValidationError("order id is required")
	}
	if len(id) > 36 {
		return newValidationError("order id exceeds 36 characters")
	}
	for i := 0; i < len(id); i++ {
		c := id[i]
		if (c < 'A' || c > 'Z') && (c < '0' || c > '9') {
			return newValidationError("order id must be [A-Z0-9]")
		}
	}
	return nil
}

func validateRequestID(id string) error {
	if id == "" {
		return newValidationError("PayPal-Request-Id is required for this operation")
	}
	if len(id) > maxRequestIDLen {
		return newValidationError("PayPal-Request-Id exceeds %d bytes", maxRequestIDLen)
	}
	for i := 0; i < len(id); i++ {
		if id[i] < 0x21 || id[i] > 0x7e {
			return newValidationError("PayPal-Request-Id contains non-printable characters")
		}
	}
	return nil
}

func validateOrderResponse(o *Order) error {
	if o.ID == "" {
		return newValidationError("provider response did not include an order id")
	}
	if o.Status == "" {
		return newValidationError("provider response did not include an order status")
	}
	return nil
}

// AmbiguousOutcome reports whether an error from a mutating provider call
// leaves the payment outcome unknown. Only a definitive 4xx provider
// rejection (or a local validation error, which means no request was sent)
// proves nothing happened; timeouts, transport failures, 5xx responses, and
// unreadable success bodies all require reconciliation, never a blind retry.
func AmbiguousOutcome(err error) bool {
	if err == nil {
		return false
	}
	var validation *ValidationError
	if errors.As(err, &validation) {
		return false
	}
	var apiErr *APIError
	if errors.As(err, &apiErr) {
		return apiErr.StatusCode >= 500
	}
	return true
}
