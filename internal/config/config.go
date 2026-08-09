// Package config loads AIHub server configuration from environment variables.
package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// Config holds all runtime configuration for the AIHub server.
type Config struct {
	// HTTPAddr is the listen address for the HTTP server, e.g. ":8080".
	HTTPAddr string
	// PublicBaseURL is the externally reachable base URL, used for links and MCP.
	PublicBaseURL string
	// DatabaseURL is the PostgreSQL DSN.
	DatabaseURL string

	// MinIO / S3-compatible storage.
	MinIOEndpoint  string
	MinIOAccessKey string
	MinIOSecretKey string
	MinIOUseSSL    bool
	MinIOBucket    string

	// Admin bootstrap.
	AdminUsername     string
	AdminPasswordFile string

	// SessionTTL is how long web sessions live.
	SessionTTL time.Duration
	// APITokenTTL is the default API token lifetime (0 = no expiry).
	APITokenTTL time.Duration
	// LoginMaxAttempts and LoginWindow bound the login rate limiter.
	LoginMaxAttempts int
	LoginWindow      time.Duration

	// MaxUploadBytes is the maximum accepted object size for MinIO uploads.
	MaxUploadBytes int64

	// EnabledModules is the set of module IDs enabled at startup. Empty means all.
	EnabledModules map[string]bool
	// ModulesEnvVar is the raw AIHUB_MODULES value.
	ModulesEnvVar string
}

// env returns the value of key or def.
func env(key, def string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return def
}

func envBool(key string, def bool) bool {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		return def
	}
	return b
}

func envInt(key string, def int) int {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return def
	}
	return n
}

func envDuration(key string, def time.Duration) time.Duration {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		return def
	}
	return d
}

// Load reads configuration from environment variables.
func Load() (*Config, error) {
	cfg := &Config{
		HTTPAddr:          env("AIHUB_HTTP_ADDR", ":8080"),
		PublicBaseURL:     env("AIHUB_PUBLIC_BASE_URL", "http://localhost:8080"),
		DatabaseURL:       env("AIHUB_DATABASE_URL", "postgres://aihub:aihub@localhost:5432/aihub?sslmode=disable"),
		MinIOEndpoint:     env("AIHUB_MINIO_ENDPOINT", "localhost:9000"),
		MinIOAccessKey:    env("AIHUB_MINIO_ACCESS_KEY", "aihub"),
		MinIOSecretKey:    env("AIHUB_MINIO_SECRET_KEY", "aihub-secret"),
		MinIOUseSSL:       envBool("AIHUB_MINIO_USE_SSL", false),
		MinIOBucket:       env("AIHUB_MINIO_BUCKET", "aihub"),
		AdminUsername:     env("ADMIN_USERNAME", "admin"),
		AdminPasswordFile: env("ADMIN_PASSWORD_FILE", ""),
		SessionTTL:        envDuration("AIHUB_SESSION_TTL", 24*time.Hour),
		APITokenTTL:       envDuration("AIHUB_TOKEN_TTL", 0),
		LoginMaxAttempts:  envInt("AIHUB_LOGIN_MAX_ATTEMPTS", 5),
		LoginWindow:       envDuration("AIHUB_LOGIN_WINDOW", 5*time.Minute),
		MaxUploadBytes:    int64(envInt("AIHUB_MAX_UPLOAD_MB", 100)) * 1024 * 1024,
		ModulesEnvVar:     os.Getenv("AIHUB_MODULES"),
	}
	if cfg.DatabaseURL == "" {
		return nil, fmt.Errorf("AIHUB_DATABASE_URL is required")
	}
	cfg.EnabledModules = map[string]bool{}
	for _, m := range strings.Split(cfg.ModulesEnvVar, ",") {
		m = strings.TrimSpace(m)
		if m != "" {
			cfg.EnabledModules[m] = true
		}
	}
	return cfg, nil
}

// ModuleEnabled reports whether module id is enabled.
func (c *Config) ModuleEnabled(id string) bool {
	if len(c.EnabledModules) == 0 {
		return true
	}
	return c.EnabledModules[id]
}
