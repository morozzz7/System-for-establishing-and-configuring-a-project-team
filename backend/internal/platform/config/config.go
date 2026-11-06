// Package config loads runtime configuration from environment variables.
// No personal data is stored; all values are connection/infra parameters.
package config

import (
	"os"
	"strconv"
)

// Config holds the runtime configuration for the backend service.
type Config struct {
	// Port the HTTP server listens on (default 8080).
	Port int
	// DatabaseURL is the pgx-compatible connection string.
	// When empty, postgres dependency is reported as "unknown" in readyz.
	DatabaseURL string
	// MLServiceURL is the base URL of the ML service (no trailing slash).
	MLServiceURL string
}

// Load reads configuration from environment variables with sensible defaults.
func Load() Config {
	return Config{
		Port:         getEnvInt("BACKEND_PORT", 8080),
		DatabaseURL:  os.Getenv("DATABASE_URL"),
		MLServiceURL: getEnvDefault("ML_SERVICE_URL", "http://ml:8000"),
	}
}

func getEnvDefault(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func getEnvInt(key string, def int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}
