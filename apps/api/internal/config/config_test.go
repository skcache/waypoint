package config

import "testing"

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
}

func TestLoadReadsEnvironment(t *testing.T) {
	env := map[string]string{"APP_ENV": "test", "API_PORT": "9090"}
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
