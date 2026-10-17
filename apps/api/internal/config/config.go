// Package config loads and validates the Waypoint API configuration from the
// process environment.
//
// Phase 01 keeps the surface deliberately small: environment name and listen
// port. Typed, fail-fast configuration for database, PayPal and LLM settings
// arrives with the phases that actually consume those values.
package config

import (
	"fmt"
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
}

// Load reads configuration from getenv (typically os.Getenv) and validates
// it. It performs no external calls and requires no credentials: the Phase 01
// scaffold starts correctly on a completely empty environment.
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

	return cfg, nil
}

// Addr returns the TCP listen address for the API.
func (c Config) Addr() string {
	return fmt.Sprintf(":%d", c.Port)
}

func valueOr(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}
