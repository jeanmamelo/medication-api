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
	defaultEnvironment     = "local"
	defaultPort            = 8080
	defaultReadHeaderLimit = 5 * time.Second
	defaultReadLimit       = 15 * time.Second
	defaultWriteLimit      = 15 * time.Second
	defaultIdleLimit       = 60 * time.Second
	defaultShutdownLimit   = 10 * time.Second

	defaultMaxConns          = 10
	defaultMinConns          = 2
	maxPoolSize              = 100
	defaultConnLifetime      = time.Hour
	defaultConnIdleTime      = 30 * time.Minute
	defaultHealthCheckPeriod = time.Minute
)

// Config contains only process-level settings. Secrets are never logged.
type Config struct {
	Environment string
	DatabaseURL string
	HTTP        HTTPConfig
	Database    DatabaseConfig
}

// DatabaseConfig bounds the connection pool. These values are authoritative, so any
// pool_* parameter in DATABASE_URL is overridden.
type DatabaseConfig struct {
	MaxConns          int32
	MinConns          int32
	MaxConnLifetime   time.Duration
	MaxConnIdleTime   time.Duration
	HealthCheckPeriod time.Duration
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
	if environment != "local" && environment != "prod" {
		return Config{}, fmt.Errorf("APP_ENV must be local or prod")
	}

	maxConns, err := poolSizeFromEnv("DB_MAX_CONNS", defaultMaxConns)
	if err != nil {
		return Config{}, err
	}
	minConns, err := poolSizeFromEnv("DB_MIN_CONNS", defaultMinConns)
	if err != nil {
		return Config{}, err
	}
	if minConns > maxConns {
		return Config{}, fmt.Errorf("DB_MIN_CONNS must not exceed DB_MAX_CONNS")
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
		Database: DatabaseConfig{
			MaxConns:          int32(maxConns),
			MinConns:          int32(minConns),
			MaxConnLifetime:   defaultConnLifetime,
			MaxConnIdleTime:   defaultConnIdleTime,
			HealthCheckPeriod: defaultHealthCheckPeriod,
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

// poolSizeFromEnv bounds connection pool sizes separately from port numbers: the two
// have unrelated valid ranges, and reusing the port bound produced a confusing error
// message ("must be between 1 and 65535") for a pool size setting.
func poolSizeFromEnv(key string, fallback int) (int, error) {
	value := valueOrDefault(key, strconv.Itoa(fallback))
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed < 1 || parsed > maxPoolSize {
		return 0, fmt.Errorf("%s must be an integer between 1 and %d", key, maxPoolSize)
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
