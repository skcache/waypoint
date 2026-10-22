// Package llm is the hosted-model adapter used by Waypoint's runtime AI
// functions: intent draft compilation now, semantic verification in its
// assigned phase.
//
// Design boundaries (deliberate):
//
//   - Standard library only (net/http + encoding/json). The wire shape is
//     the OpenAI-compatible chat-completions API; the transport stays
//     replaceable behind the intent.Compiler interface, so swapping the
//     provider never touches product logic.
//   - The API key never leaves the process: it is sent as a bearer header,
//     never logged, never embedded in errors, never returned to callers.
//   - Provider failures are classified, not guessed at: a rejection (the
//     provider refused the request before processing it) is told apart from
//     unavailability (timeouts, throttling, 5xx — the request may have
//     been partially processed). Callers decide retry policy from the kind.
//   - Plain HTTP is allowed only for loopback development servers; every
//     other base URL must be HTTPS.
package llm

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	// chatCompletionsPath is appended to the configured base URL.
	chatCompletionsPath = "/chat/completions"

	// defaultTimeout bounds one model call end to end.
	defaultTimeout = 30 * time.Second

	// maxResponseBytes bounds how much provider response will be read.
	maxResponseBytes = 256 << 10

	// maxOutputTokens bounds the completion size. Draft JSON is small; the
	// cap protects against runaway generations and surprise cost.
	maxOutputTokens = 1000
)

// ErrNotConfigured reports absent or incomplete LLM configuration. Callers
// surface this as NOT_CONFIGURED and must not fabricate model results.
var ErrNotConfigured = errors.New("llm: NOT_CONFIGURED")

// Config is the model-provider configuration, loaded only from the
// environment (LLM_API_BASE_URL, LLM_API_KEY, LLM_MODEL — PRD §5.4).
type Config struct {
	// BaseURL is the provider API root, e.g. https://api.example.com/v1.
	// Plain http:// is accepted only for loopback development servers.
	BaseURL string
	// APIKey is the bearer credential. Opaque here; never logged or echoed.
	APIKey string
	// Model names the model to call.
	Model string
	// Timeout bounds one model call. Zero means the adapter default (30s).
	Timeout time.Duration
	// MaxOutputTokens bounds the completion. Zero means the adapter default
	// (1000); reasoning models need headroom because hidden reasoning counts
	// against the budget.
	MaxOutputTokens int
	// ReasoningEffort optionally asks a reasoning-capable provider to spend
	// less hidden reasoning (low|medium|high). Empty leaves the provider
	// default. It never changes what the model is allowed to see.
	ReasoningEffort string
}

// Configured reports whether every required value is present.
func (c Config) Configured() bool {
	return c.BaseURL != "" && c.APIKey != "" && c.Model != ""
}

// ConfigFromEnv loads the LLM configuration. Missing values yield an error
// wrapping ErrNotConfigured; malformed values are rejected early. Error
// messages never repeat configuration values.
func ConfigFromEnv(getenv func(string) string) (Config, error) {
	cfg := Config{
		BaseURL: getenv("LLM_API_BASE_URL"),
		APIKey:  getenv("LLM_API_KEY"),
		Model:   getenv("LLM_MODEL"),
	}
	if !cfg.Configured() {
		return Config{}, fmt.Errorf("%w: LLM_API_BASE_URL, LLM_API_KEY and LLM_MODEL must all be set", ErrNotConfigured)
	}
	if err := ValidateBaseURL(cfg.BaseURL); err != nil {
		return Config{}, err
	}
	if err := ValidateModel(cfg.Model); err != nil {
		return Config{}, err
	}
	if raw := getenv("LLM_TIMEOUT_SECONDS"); raw != "" {
		secs, err := strconv.Atoi(raw)
		if err != nil || secs < 10 || secs > 600 {
			return Config{}, fmt.Errorf("invalid LLM_TIMEOUT_SECONDS (value redacted): want an integer between 10 and 600")
		}
		cfg.Timeout = time.Duration(secs) * time.Second
	}
	if raw := getenv("LLM_MAX_OUTPUT_TOKENS"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 100 || n > 16000 {
			return Config{}, fmt.Errorf("invalid LLM_MAX_OUTPUT_TOKENS (value redacted): want an integer between 100 and 16000")
		}
		cfg.MaxOutputTokens = n
	}
	if raw := getenv("LLM_REASONING_EFFORT"); raw != "" {
		switch raw {
		case "low", "medium", "high":
			cfg.ReasoningEffort = raw
		default:
			return Config{}, fmt.Errorf("invalid LLM_REASONING_EFFORT (value redacted): want low, medium or high")
		}
	}
	return cfg, nil
}

// ValidateBaseURL rejects base URLs this adapter must not talk to: non-HTTP
// schemes, embedded credentials, queries or fragments, and plain HTTP
// outside loopback. Messages never echo the value.
func ValidateBaseURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return errors.New("invalid LLM_API_BASE_URL (value redacted): not a valid URL")
	}
	if u.Scheme != "https" && u.Scheme != "http" {
		return fmt.Errorf("invalid LLM_API_BASE_URL (value redacted): unsupported scheme %q; want https:// (http:// only for loopback development)", u.Scheme)
	}
	if u.Host == "" {
		return errors.New("invalid LLM_API_BASE_URL (value redacted): missing host")
	}
	if u.User != nil {
		return errors.New("invalid LLM_API_BASE_URL (value redacted): must not carry credentials")
	}
	if u.RawQuery != "" || u.Fragment != "" {
		return errors.New("invalid LLM_API_BASE_URL (value redacted): must not include a query or fragment")
	}
	if u.Scheme == "http" && !isLoopbackHost(u.Hostname()) {
		return errors.New("invalid LLM_API_BASE_URL (value redacted): plain http is allowed only for loopback hosts")
	}
	return nil
}

// ValidateModel checks the model name shape. The value never appears in
// error messages.
func ValidateModel(model string) error {
	if model == "" || len(model) > 200 {
		return errors.New("invalid LLM_MODEL (value redacted): must be 1..200 printable characters")
	}
	for i := 0; i < len(model); i++ {
		if model[i] < 0x21 || model[i] > 0x7e {
			return errors.New("invalid LLM_MODEL (value redacted): must be printable ASCII without spaces")
		}
	}
	return nil
}

func isLoopbackHost(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	if ip := net.ParseIP(host); ip != nil {
		return ip.IsLoopback()
	}
	return false
}

// ErrorKind classifies a provider failure.
type ErrorKind string

const (
	// KindUnavailable: timeout, transport failure, throttling (429), 5xx, or
	// a 2xx whose body cannot be read. The request may or may not have been
	// processed; retrying is safe for read-only calls.
	KindUnavailable ErrorKind = "UNAVAILABLE"
	// KindRejected: the provider refused the request before processing it
	// (bad key, bad model, unacceptable request). Retrying the same request
	// will not help; the operator must fix configuration.
	KindRejected ErrorKind = "REJECTED"
)

// Error is a sanitized provider failure. It carries a classification and a
// short structural detail — never response bodies, headers, URLs or
// credentials.
type Error struct {
	Kind   ErrorKind
	Status int // provider HTTP status when one was received; 0 otherwise
	Detail string
}

func (e *Error) Error() string {
	if e.Status > 0 {
		return fmt.Sprintf("llm: %s (HTTP %d): %s", e.Kind, e.Status, e.Detail)
	}
	return fmt.Sprintf("llm: %s: %s", e.Kind, e.Detail)
}

// Retryable reports whether retrying the same request can change the result.
func (e *Error) Retryable() bool { return e.Kind == KindUnavailable }

// classifyStatus mirrors the provider rejection semantics used by the PayPal
// adapter: statuses that refuse a request before processing it are definite
// rejections. 408 (request timeout) and 429 (throttling) deliberately stay
// unavailable — they prove nothing about how far the request got.
func classifyStatus(status int) ErrorKind {
	switch status {
	case http.StatusBadRequest, http.StatusUnauthorized, http.StatusForbidden,
		http.StatusNotFound, http.StatusMethodNotAllowed, http.StatusNotAcceptable,
		http.StatusUnsupportedMediaType, http.StatusUnprocessableEntity:
		return KindRejected
	}
	return KindUnavailable
}

// Client is a minimal hosted chat-completions client.
type Client struct {
	base      *url.URL
	apiKey    string
	model     string
	maxTokens int
	reasoning string
	http      *http.Client
}

// New validates the configuration and builds a client with bounded
// timeouts and a redirect policy that refuses cross-host or scheme-changing
// redirects.
func New(cfg Config) (*Client, error) {
	if !cfg.Configured() {
		return nil, fmt.Errorf("%w: base URL, API key and model are all required", ErrNotConfigured)
	}
	if err := ValidateBaseURL(cfg.BaseURL); err != nil {
		return nil, err
	}
	if err := ValidateModel(cfg.Model); err != nil {
		return nil, err
	}
	u, err := url.Parse(cfg.BaseURL)
	if err != nil {
		// Unreachable: ValidateBaseURL already parsed the value.
		return nil, fmt.Errorf("%w: base URL is not parseable", ErrNotConfigured)
	}
	maxTokens := cfg.MaxOutputTokens
	if maxTokens <= 0 {
		maxTokens = maxOutputTokens
	}
	c := &Client{base: u, apiKey: cfg.APIKey, model: cfg.Model, maxTokens: maxTokens}
	c.http = &http.Client{
		Timeout:       clientTimeout(cfg),
		CheckRedirect: c.checkRedirect,
		Transport: &http.Transport{
			TLSClientConfig:     &tls.Config{MinVersion: tls.VersionTLS12},
			MaxIdleConnsPerHost: 4,
		},
	}
	return c, nil
}

// newWithClient exists so tests can substitute an HTTP client (shorter
// timeouts, trusted test certificates). The redirect policy still applies.
func newWithClient(cfg Config, hc *http.Client) (*Client, error) {
	c, err := New(cfg)
	if err != nil {
		return nil, err
	}
	if hc.CheckRedirect == nil {
		hc.CheckRedirect = c.checkRedirect
	}
	c.http = hc
	return c, nil
}

func clientTimeout(cfg Config) time.Duration {
	if cfg.Timeout > 0 {
		return cfg.Timeout
	}
	return defaultTimeout
}

func (c *Client) checkRedirect(req *http.Request, via []*http.Request) error {
	if len(via) == 0 {
		return nil
	}
	if len(via) >= 3 {
		return errors.New("llm: too many redirects")
	}
	first := via[0].URL
	if req.URL.Host != first.Host {
		return errors.New("llm: refusing cross-host redirect")
	}
	if req.URL.Scheme != first.Scheme {
		return errors.New("llm: refusing redirect that changes scheme")
	}
	if first.Scheme != "https" && !isLoopbackHost(first.Hostname()) {
		return errors.New("llm: refusing redirect off HTTPS")
	}
	return nil
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatRequest struct {
	Model       string           `json:"model"`
	Messages    []chatMessage    `json:"messages"`
	Temperature float64          `json:"temperature"`
	MaxTokens   int              `json:"max_tokens"`
	Reasoning   *reasoningConfig `json:"reasoning,omitempty"`
}

type reasoningConfig struct {
	Effort string `json:"effort"`
}

// chatCompletionResponse is the envelope. Unknown envelope fields are
// tolerated deliberately: providers evolve their metadata (usage, ids)
// without notice, and refusing them would break on provider upgrades. The
// strict schema enforcement lives one layer up, on the extracted draft.
type chatCompletionResponse struct {
	Choices []struct {
		FinishReason string `json:"finish_reason"`
		Message      struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
	Usage *struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
		TotalTokens      int `json:"total_tokens"`
	} `json:"usage"`
	Error *struct {
		Message string `json:"message"`
		Type    string `json:"type"`
	} `json:"error"`
}

// Completion is one assistant reply plus whatever usage metadata the provider
// reported. Usage is optional by design: providers evolve their envelopes, and
// a missing usage block must not turn a usable reply into an error.
type Completion struct {
	Text             string
	PromptTokens     int
	CompletionTokens int
	UsageReported    bool
}

// Generate performs one chat completion and returns the assistant message
// text. temperature 0 is requested for run-to-run stability; providers are
// still stochastic, which is exactly why every reply is validated strictly
// by the caller before it is shown as a draft.
func (c *Client) Generate(ctx context.Context, system, user string) (string, error) {
	out, err := c.GenerateDetail(ctx, system, user)
	if err != nil {
		return "", err
	}
	return out.Text, nil
}

// GenerateDetail performs one chat completion and returns the assistant text
// with the provider's usage numbers when present.
func (c *Client) GenerateDetail(ctx context.Context, system, user string) (Completion, error) {
	reqBody := chatRequest{
		Model: c.model,
		Messages: []chatMessage{
			{Role: "system", Content: system},
			{Role: "user", Content: user},
		},
		Temperature: 0,
		MaxTokens:   c.maxTokens,
	}
	if c.reasoning != "" {
		reqBody.Reasoning = &reasoningConfig{Effort: c.reasoning}
	}
	payload, err := json.Marshal(reqBody)
	if err != nil {
		return Completion{}, &Error{Kind: KindUnavailable, Detail: "request encoding failed"}
	}

	endpoint := strings.TrimRight(c.base.String(), "/") + chatCompletionsPath
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return Completion{}, &Error{Kind: KindUnavailable, Detail: "request construction failed"}
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.apiKey)

	resp, err := c.http.Do(req)
	if err != nil {
		// Classification only — the raw transport error is not embedded:
		// it can carry the request URL, and callers must not echo internals.
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
			return Completion{}, &Error{Kind: KindUnavailable, Detail: "the request did not complete in time"}
		}
		var netErr net.Error
		if errors.As(err, &netErr) && netErr.Timeout() {
			return Completion{}, &Error{Kind: KindUnavailable, Detail: "the request timed out"}
		}
		return Completion{}, &Error{Kind: KindUnavailable, Detail: "the request failed before a response arrived"}
	}
	defer func() {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
		_ = resp.Body.Close()
	}()

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		kind := classifyStatus(resp.StatusCode)
		detail := "the provider did not complete the request"
		if kind == KindRejected {
			detail = "the provider rejected the request"
		}
		return Completion{}, &Error{Kind: kind, Status: resp.StatusCode, Detail: detail}
	}

	var envelope chatCompletionResponse
	dec := json.NewDecoder(io.LimitReader(resp.Body, maxResponseBytes))
	if err := dec.Decode(&envelope); err != nil {
		return Completion{}, &Error{Kind: KindUnavailable, Status: resp.StatusCode, Detail: "the provider response could not be read"}
	}
	if _, err := dec.Token(); err != io.EOF {
		return Completion{}, &Error{Kind: KindUnavailable, Status: resp.StatusCode, Detail: "the provider response could not be read"}
	}
	if envelope.Error != nil && envelope.Error.Message != "" {
		return Completion{}, &Error{Kind: KindUnavailable, Status: resp.StatusCode, Detail: "the provider reported an error"}
	}
	if len(envelope.Choices) == 0 || strings.TrimSpace(envelope.Choices[0].Message.Content) == "" {
		detail := "the provider response carried no assistant text"
		if len(envelope.Choices) > 0 && envelope.Choices[0].FinishReason == "length" {
			detail = "the provider exhausted the output budget before emitting text (raise LLM_MAX_OUTPUT_TOKENS)"
		}
		return Completion{}, &Error{Kind: KindUnavailable, Status: resp.StatusCode, Detail: detail}
	}
	out := Completion{Text: envelope.Choices[0].Message.Content}
	if envelope.Usage != nil {
		out.PromptTokens = envelope.Usage.PromptTokens
		out.CompletionTokens = envelope.Usage.CompletionTokens
		out.UsageReported = true
	}
	return out, nil
}
