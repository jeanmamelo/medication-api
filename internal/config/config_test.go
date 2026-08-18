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

	if cfg.Environment != "local" {
		t.Errorf("Environment = %q, want local", cfg.Environment)
	}
	if cfg.HTTP.Address != ":8080" {
		t.Errorf("Address = %q, want :8080", cfg.HTTP.Address)
	}
	if cfg.HTTP.ReadHeaderTimeout != 5*time.Second {
		t.Errorf("ReadHeaderTimeout = %s, want 5s", cfg.HTTP.ReadHeaderTimeout)
	}
	if cfg.Database.MaxConns != 10 || cfg.Database.MinConns != 2 {
		t.Errorf("pool bounds = %d/%d, want 10/2", cfg.Database.MaxConns, cfg.Database.MinConns)
	}
	if cfg.Database.MaxConnLifetime != time.Hour || cfg.Database.MaxConnIdleTime != 30*time.Minute || cfg.Database.HealthCheckPeriod != time.Minute {
		t.Errorf("pool lifetimes = %#v", cfg.Database)
	}
}

func TestLoadOverridesPoolBounds(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://user:password@localhost:5432/medications")
	t.Setenv("DB_MAX_CONNS", "25")
	t.Setenv("DB_MIN_CONNS", "5")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.Database.MaxConns != 25 || cfg.Database.MinConns != 5 {
		t.Errorf("pool bounds = %d/%d, want 25/5", cfg.Database.MaxConns, cfg.Database.MinConns)
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
		{name: "invalid max connections", key: "DB_MAX_CONNS", value: "zero"},
		{name: "non-positive min connections", key: "DB_MIN_CONNS", value: "0"},
		{name: "out of range max connections", key: "DB_MAX_CONNS", value: "101"},
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

func TestLoadRejectsMinConnsAboveMaxConns(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://user:password@localhost:5432/medications")
	t.Setenv("DB_MAX_CONNS", "4")
	t.Setenv("DB_MIN_CONNS", "8")

	if _, err := Load(); err == nil {
		t.Fatal("Load() error = nil, want an error")
	}
}
