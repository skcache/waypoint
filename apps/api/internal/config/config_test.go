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
