// Package config loads and validates the Waypoint API configuration from the
// process environment.
//
// Configuration is minimal and fail-fast for values that are provided, while
// staying optional overall: the process starts correctly with an empty
// environment. Features whose dependencies are not configured report
// NOT_CONFIGURED rather than pretending to work.
package config

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/skcache/waypoint/apps/api/internal/llm"
)

const (
	defaultAppEnv = "development"
	defaultPort   = 8080
)

// Config is the runtime configuration for the API process.
type Config struct {
	// AppEnv names the environment; "development" unless overridden.
	AppEnv string
	// Port is the TCP port the API listens on.
	Port int
	// DatabaseURL is the PostgreSQL DSN. Empty means "not configured":
	// /healthz still works and /readyz reports not_configured.
	DatabaseURL string
	// WebOrigin is the single browser origin allowed on write endpoints.
	// Empty disables the origin check (server-to-server use only).
	WebOrigin string
	// LLMBaseURL, LLMAPIKey and LLMModel configure the runtime AI adapter
	// (intent draft compilation now; semantic verification later). All three
	// must be present together — see LLMConfigured. The key is never logged
	// or included in error messages.
	LLMBaseURL string
	LLMAPIKey  string
	LLMModel   string
}

// LLMConfigured reports whether every runtime-AI value is present. A partial
// configuration is not an error at startup: the features that need it report
// NOT_CONFIGURED instead of pretending to work.
func (c Config) LLMConfigured() bool {
	return c.LLMBaseURL != "" && c.LLMAPIKey != "" && c.LLMModel != ""
}

// Load reads configuration from getenv (typically os.Getenv) and validates
// it. It performs no external calls and requires no credentials.
func Load(getenv func(string) string) (Config, error) {
	cfg := Config{
		AppEnv: valueOr(getenv("APP_ENV"), defaultAppEnv),
		Port:   defaultPort,
	}

	if raw := getenv("API_PORT"); raw != "" {
		port, err := strconv.Atoi(raw)
		if err != nil || port < 1 || port > 65535 {
			return Config{}, fmt.Errorf("invalid API_PORT %q: want an integer between 1 and 65535", raw)
		}
		cfg.Port = port
	}

	if dsn := getenv("DATABASE_URL"); dsn != "" {
		if err := validateDatabaseURL(dsn); err != nil {
			return Config{}, err
		}
		cfg.DatabaseURL = dsn
	}

	if origin := getenv("WEB_ORIGIN"); origin != "" {
		if err := validateWebOrigin(origin); err != nil {
			return Config{}, err
		}
		cfg.WebOrigin = origin
	}

	if base := getenv("LLM_API_BASE_URL"); base != "" {
		if err := llm.ValidateBaseURL(base); err != nil {
			return Config{}, err
		}
		cfg.LLMBaseURL = base
	}
	cfg.LLMAPIKey = getenv("LLM_API_KEY")
	if model := getenv("LLM_MODEL"); model != "" {
		if err := llm.ValidateModel(model); err != nil {
			return Config{}, err
		}
		cfg.LLMModel = model
	}

	return cfg, nil
}

// Addr returns the TCP listen address for the API.
func (c Config) Addr() string {
	return fmt.Sprintf(":%d", c.Port)
}

// validateDatabaseURL rejects non-PostgreSQL schemes and malformed values
// early. Error messages never repeat the value: configuration errors are
// logged, and a DSN may embed a password.
func validateDatabaseURL(dsn string) error {
	u, err := url.Parse(dsn)
	if err != nil {
		return fmt.Errorf("invalid DATABASE_URL (value redacted): not a valid URL")
	}
	if u.Scheme != "postgres" && u.Scheme != "postgresql" {
		return fmt.Errorf("invalid DATABASE_URL (value redacted): unsupported scheme %q; want postgres:// or postgresql://", u.Scheme)
	}
	if u.Host == "" {
		return fmt.Errorf("invalid DATABASE_URL (value redacted): missing host")
	}
	return nil
}

// validateWebOrigin checks the single allowed browser origin. It must be a
// bare http(s) origin (scheme + host [+ port]) with no path, credentials,
// query or fragment — exactly what a browser puts in an Origin header.
func validateWebOrigin(origin string) error {
	u, err := url.Parse(origin)
	if err != nil {
		return fmt.Errorf("invalid WEB_ORIGIN (value redacted): not a valid URL")
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("invalid WEB_ORIGIN (value redacted): unsupported scheme %q; want http:// or https://", u.Scheme)
	}
	if u.Host == "" {
		return fmt.Errorf("invalid WEB_ORIGIN (value redacted): missing host")
	}
	if u.User != nil {
		return fmt.Errorf("invalid WEB_ORIGIN (value redacted): must not carry credentials")
	}
	if (u.Path != "" && u.Path != "/") || u.RawQuery != "" || u.Fragment != "" {
		return fmt.Errorf("invalid WEB_ORIGIN (value redacted): must be a bare origin without path, query or fragment")
	}
	if strings.HasSuffix(origin, "/") {
		return fmt.Errorf("invalid WEB_ORIGIN (value redacted): must not end with a slash")
	}
	return nil
}

func valueOr(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}
