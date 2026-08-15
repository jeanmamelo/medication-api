// Package config loads and validates application configuration from the environment.
package config

import (
	"fmt"
	"net"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	defaultEnvironment     = "dev"
	defaultPort            = 8080
	defaultReadHeaderLimit = 5 * time.Second
	defaultReadLimit       = 15 * time.Second
	defaultWriteLimit      = 15 * time.Second
	defaultIdleLimit       = 60 * time.Second
	defaultShutdownLimit   = 10 * time.Second
)

// Config contains only process-level settings. Secrets are never logged.
type Config struct {
	Environment string
	DatabaseURL string
	HTTP        HTTPConfig
}

// HTTPConfig controls the network-facing server limits.
type HTTPConfig struct {
	Address           string
	ReadHeaderTimeout time.Duration
	ReadTimeout       time.Duration
	WriteTimeout      time.Duration
	IdleTimeout       time.Duration
	ShutdownTimeout   time.Duration
}

// Load reads configuration from environment variables and fails fast on invalid values.
func Load() (Config, error) {
	port, err := positiveIntFromEnv("PORT", defaultPort)
	if err != nil {
		return Config{}, err
	}
	databaseURL := strings.TrimSpace(os.Getenv("DATABASE_URL"))
	if databaseURL == "" {
		return Config{}, fmt.Errorf("DATABASE_URL is required")
	}
	parsedDatabaseURL, err := url.Parse(databaseURL)
	if err != nil || (parsedDatabaseURL.Scheme != "postgres" && parsedDatabaseURL.Scheme != "postgresql") || parsedDatabaseURL.Host == "" {
		return Config{}, fmt.Errorf("DATABASE_URL must be a valid PostgreSQL URL")
	}

	environment := valueOrDefault("APP_ENV", defaultEnvironment)
	if environment != "dev" && environment != "hom" && environment != "prod" {
		return Config{}, fmt.Errorf("APP_ENV must be dev, hom, or prod")
	}

	return Config{
		Environment: environment,
		DatabaseURL: databaseURL,
		HTTP: HTTPConfig{
			Address:           net.JoinHostPort("", strconv.Itoa(port)),
			ReadHeaderTimeout: defaultReadHeaderLimit,
			ReadTimeout:       defaultReadLimit,
			WriteTimeout:      defaultWriteLimit,
			IdleTimeout:       defaultIdleLimit,
			ShutdownTimeout:   defaultShutdownLimit,
		},
	}, nil
}

func positiveIntFromEnv(key string, fallback int) (int, error) {
	value := valueOrDefault(key, strconv.Itoa(fallback))
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed < 1 || parsed > 65535 {
		return 0, fmt.Errorf("%s must be an integer between 1 and 65535", key)
	}

	return parsed, nil
}

func valueOrDefault(key, fallback string) string {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}

	return value
}
