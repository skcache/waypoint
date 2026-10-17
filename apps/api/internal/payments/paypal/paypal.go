// Package paypal is a minimal, sandbox-only PayPal REST client used by the
// Phase 04 feasibility spike.
//
// Scope and boundaries (deliberate):
//
//   - Sandbox only. The base URL is a constant pinned to
//     https://api-m.sandbox.paypal.com; there is no configuration surface
//     that could point this client at production.
//   - Standard library only (net/http + encoding/json). No SDK.
//   - Secrets never leave the process: client credentials are sent exactly
//     once per token acquisition via HTTP Basic auth, and bearer tokens are
//     never logged, returned to callers, or embedded in errors.
//   - This package is NOT the Waypoint payment coordinator. It creates and
//     captures standalone provider orders for feasibility evidence; it does
//     not touch the domain state machine or the database.
package paypal

import (
	"errors"
	"fmt"
	"time"
)

const (
	// SandboxBaseURL is the only PayPal REST base this client will ever use.
	SandboxBaseURL = "https://api-m.sandbox.paypal.com"

	// EnvironmentSandbox is the only accepted PAYPAL_ENV value.
	EnvironmentSandbox = "sandbox"

	// tokenSafetyMargin refreshes a cached token this long before the
	// provider-declared expiry, so an in-flight request never rides an
	// almost-expired token.
	tokenSafetyMargin = 60 * time.Second

	// maxResponseBytes bounds how much provider response we will read.
	maxResponseBytes = 1 << 20
)

// ErrNotConfigured reports absent or incomplete credentials. Callers should
// surface this as NOT_CONFIGURED and must not fabricate success.
var ErrNotConfigured = errors.New("paypal: NOT_CONFIGURED")

// ErrUnsupportedEnvironment reports a PAYPAL_ENV value other than "sandbox".
var ErrUnsupportedEnvironment = errors.New("paypal: only PAYPAL_ENV=sandbox is supported")

// Config is the sandbox credential set, loaded only from the environment.
type Config struct {
	// Environment must be "sandbox".
	Environment string
	// ClientID and ClientSecret are the sandbox application credentials.
	ClientID     string
	ClientSecret string
}

// ConfigFromEnv loads and validates the sandbox configuration. It performs no
// network calls. Error messages never repeat credential values.
func ConfigFromEnv(getenv func(string) string) (Config, error) {
	env := getenv("PAYPAL_ENV")
	if env == "" {
		return Config{}, fmt.Errorf("%w: PAYPAL_ENV is not set (use PAYPAL_ENV=sandbox)", ErrNotConfigured)
	}
	if env != EnvironmentSandbox {
		return Config{}, fmt.Errorf("%w (got PAYPAL_ENV=%q)", ErrUnsupportedEnvironment, env)
	}
	clientID := getenv("PAYPAL_CLIENT_ID")
	clientSecret := getenv("PAYPAL_CLIENT_SECRET")
	if clientID == "" || clientSecret == "" {
		return Config{}, fmt.Errorf("%w: PAYPAL_CLIENT_ID and PAYPAL_CLIENT_SECRET must both be set", ErrNotConfigured)
	}
	return Config{Environment: env, ClientID: clientID, ClientSecret: clientSecret}, nil
}

// ValidationError marks malformed local input or a provider response that
// failed contract checks. Unlike transport or 5xx failures, a validation
// error means no outbound side effect was attempted or observed to succeed —
// callers may treat it as a definite (not ambiguous) outcome.
type ValidationError struct {
	msg string
}

func (e *ValidationError) Error() string { return "paypal: " + e.msg }

func newValidationError(format string, args ...any) *ValidationError {
	return &ValidationError{msg: fmt.Sprintf(format, args...)}
}

// APIError is a sanitized PayPal REST error. It carries only provider-supplied
// diagnostic fields (name, message, issue codes, debug id) — never request
// headers, credentials, or raw response bodies.
type APIError struct {
	StatusCode int
	Name       string
	Message    string
	DebugID    string
	Issues     []string
}

func (e *APIError) Error() string {
	msg := fmt.Sprintf("paypal API error HTTP %d", e.StatusCode)
	if e.Name != "" {
		msg += " " + e.Name
	}
	if e.Message != "" {
		msg += ": " + e.Message
	}
	if len(e.Issues) > 0 {
		msg += " [issues: " + fmt.Sprint(e.Issues) + "]"
	}
	if e.DebugID != "" {
		msg += " (debug_id " + e.DebugID + ")"
	}
	return msg
}
