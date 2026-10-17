package config

import (
	"strings"
	"testing"
)

func TestLoadDefaultsOnEmptyEnvironment(t *testing.T) {
	cfg, err := Load(func(string) string { return "" })
	if err != nil {
		t.Fatalf("Load on empty environment: %v", err)
	}
	if cfg.AppEnv != "development" {
		t.Errorf("AppEnv = %q, want %q", cfg.AppEnv, "development")
	}
	if cfg.Port != 8080 {
		t.Errorf("Port = %d, want 8080", cfg.Port)
	}
	if cfg.Addr() != ":8080" {
		t.Errorf("Addr() = %q, want %q", cfg.Addr(), ":8080")
	}
	if cfg.DatabaseURL != "" {
		t.Errorf("DatabaseURL = %q, want empty", cfg.DatabaseURL)
	}
}

func TestLoadReadsEnvironment(t *testing.T) {
	env := map[string]string{
		"APP_ENV":      "test",
		"API_PORT":     "9090",
		"DATABASE_URL": "postgres://user:pw@localhost:5433/waypoint_test?sslmode=disable",
	}
	cfg, err := Load(func(key string) string { return env[key] })
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.AppEnv != "test" {
		t.Errorf("AppEnv = %q, want %q", cfg.AppEnv, "test")
	}
	if cfg.Addr() != ":9090" {
		t.Errorf("Addr() = %q, want %q", cfg.Addr(), ":9090")
	}
	if cfg.DatabaseURL != env["DATABASE_URL"] {
		t.Errorf("DatabaseURL was not preserved")
	}
}

func TestLoadRejectsInvalidPort(t *testing.T) {
	for _, raw := range []string{"abc", "0", "-1", "65536", "8080.5"} {
		_, err := Load(func(key string) string {
			if key == "API_PORT" {
				return raw
			}
			return ""
		})
		if err == nil {
			t.Errorf("API_PORT=%q: expected an error, got none", raw)
		}
	}
}

// TestLoadRejectsNonPostgresDatabaseURL also asserts that the error message
// never repeats the DSN: configuration errors end up in logs, and a DSN may
// embed a password.
func TestLoadRejectsNonPostgresDatabaseURL(t *testing.T) {
	secret := "supersecretpw"
	_, err := Load(func(key string) string {
		if key == "DATABASE_URL" {
			return "mysql://user:" + secret + "@localhost/db"
		}
		return ""
	})
	if err == nil {
		t.Fatal("expected an error for a non-postgres scheme, got none")
	}
	if strings.Contains(err.Error(), secret) {
		t.Fatalf("error message leaks the password: %q", err.Error())
	}
	if strings.Contains(err.Error(), "mysql://user") {
		t.Fatalf("error message repeats the DSN: %q", err.Error())
	}
}

func TestLoadRejectsMalformedDatabaseURL(t *testing.T) {
	for _, raw := range []string{
		"postgres://",                 // missing host
		"postgres://user:pw@/db",      // missing host
		"://not-a-url",                // not parseable
		"postgres://user:pw@h/db\x7f", // invalid control byte
	} {
		_, err := Load(func(key string) string {
			if key == "DATABASE_URL" {
				return raw
			}
			return ""
		})
		if err == nil {
			t.Errorf("DATABASE_URL=%q: expected an error, got none", raw)
		}
		if err != nil && strings.Contains(err.Error(), "pw") {
			t.Errorf("DATABASE_URL=%q: error message leaks credentials: %q", raw, err.Error())
		}
	}
}

// TestLoadLLMConfiguration covers the runtime-AI environment contract:
// complete values load, partial values leave LLMConfigured false (the draft
// endpoint then reports NOT_CONFIGURED), and malformed values are rejected
// without echoing the key.
func TestLoadLLMConfiguration(t *testing.T) {
	full := map[string]string{
		"LLM_API_BASE_URL": "https://api.example.com/v1",
		"LLM_API_KEY":      "sk-config-test-SUPERSECRET-123456",
		"LLM_MODEL":        "model-x",
	}
	cfg, err := Load(func(k string) string { return full[k] })
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !cfg.LLMConfigured() {
		t.Error("LLMConfigured() = false for a complete configuration")
	}
	if cfg.LLMAPIKey != full["LLM_API_KEY"] {
		t.Error("API key was not preserved")
	}

	for _, missing := range []string{"LLM_API_BASE_URL", "LLM_API_KEY", "LLM_MODEL"} {
		env := map[string]string{}
		for k, v := range full {
			if k != missing {
				env[k] = v
			}
		}
		cfg, err := Load(func(k string) string { return env[k] })
		if err != nil {
			t.Errorf("partial config (missing %s) failed Load: %v", missing, err)
		}
		if cfg.LLMConfigured() {
			t.Errorf("LLMConfigured() = true with %s missing", missing)
		}
	}

	bad := map[string]string{
		"LLM_API_BASE_URL": "https://user:pass@api.example.com",
		"LLM_API_KEY":      "sk-config-test-SUPERSECRET-123456",
		"LLM_MODEL":        "model-x",
	}
	_, err = Load(func(k string) string { return bad[k] })
	if err == nil {
		t.Fatal("credential-bearing LLM base URL accepted")
	}
	if strings.Contains(err.Error(), "pass") || strings.Contains(err.Error(), bad["LLM_API_KEY"]) {
		t.Fatalf("error leaks configuration values: %q", err.Error())
	}

	badModel := map[string]string{
		"LLM_API_BASE_URL": "https://api.example.com",
		"LLM_API_KEY":      "sk-config-test-SUPERSECRET-123456",
		"LLM_MODEL":        "bad model",
	}
	_, err = Load(func(k string) string { return badModel[k] })
	if err == nil {
		t.Fatal("model name with a space accepted")
	}
	if strings.Contains(err.Error(), "bad model") || strings.Contains(err.Error(), badModel["LLM_API_KEY"]) {
		t.Fatalf("error leaks configuration values: %q", err.Error())
	}
}

func TestLoadWebOrigin(t *testing.T) {
	cfg, err := Load(func(k string) string {
		if k == "WEB_ORIGIN" {
			return "http://localhost:3000"
		}
		return ""
	})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.WebOrigin != "http://localhost:3000" {
		t.Errorf("WebOrigin = %q", cfg.WebOrigin)
	}

	for _, raw := range []string{
		"http://localhost:3000/",        // trailing slash never appears in an Origin header
		"http://localhost:3000/app",     // path
		"http://user:pw@localhost:3000", // credentials
		"ftp://localhost:3000",          // scheme
		"not a url",
	} {
		_, err := Load(func(k string) string {
			if k == "WEB_ORIGIN" {
				return raw
			}
			return ""
		})
		if err == nil {
			t.Errorf("WEB_ORIGIN=%q accepted", raw)
		}
		if err != nil && strings.Contains(err.Error(), "pw") {
			t.Errorf("WEB_ORIGIN=%q: error leaks credentials: %q", raw, err.Error())
		}
	}
}
