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

func valueOr(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}
