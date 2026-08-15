package config

import (
	"testing"
	"time"
)

func TestLoadUsesSecureDefaults(t *testing.T) {
	t.Setenv("PORT", "")
	t.Setenv("APP_ENV", "")
	t.Setenv("DATABASE_URL", "postgres://user:password@localhost:5432/medications")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.Environment != "dev" {
		t.Errorf("Environment = %q, want dev", cfg.Environment)
	}
	if cfg.HTTP.Address != ":8080" {
		t.Errorf("Address = %q, want :8080", cfg.HTTP.Address)
	}
	if cfg.HTTP.ReadHeaderTimeout != 5*time.Second {
		t.Errorf("ReadHeaderTimeout = %s, want 5s", cfg.HTTP.ReadHeaderTimeout)
	}
}

func TestLoadRejectsInvalidValues(t *testing.T) {
	tests := []struct {
		name  string
		key   string
		value string
	}{
		{name: "invalid port", key: "PORT", value: "not-a-port"},
		{name: "out of range port", key: "PORT", value: "65536"},
		{name: "invalid environment", key: "APP_ENV", value: "staging"},
		{name: "missing database URL", key: "DATABASE_URL", value: ""},
		{name: "invalid database URL", key: "DATABASE_URL", value: "https://example.com"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv(test.key, test.value)
			if test.key != "DATABASE_URL" {
				t.Setenv("DATABASE_URL", "postgres://user:password@localhost:5432/medications")
			}

			if _, err := Load(); err == nil {
				t.Fatal("Load() error = nil, want an error")
			}
		})
	}
}
