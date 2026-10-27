// Package config loads and validates application configuration from
// environment variables.
package config

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"time"
)

// minAPIKeyLength is the minimum accepted length of API_KEY.
const minAPIKeyLength = 32

// Config holds the runtime configuration of the service.
type Config struct {
	// HTTPAddr is the listen address of the API server, e.g. ":8080".
	HTTPAddr string
	// DatabaseURL is the PostgreSQL connection string.
	DatabaseURL string
	// DBMaxConns is the maximum size of the database connection pool.
	DBMaxConns int32
	// APIKey is the bearer token required on all /api/v1 endpoints.
	APIKey string
	// LogLevel controls the minimum log level.
	LogLevel slog.Level

	// UpstreamTimeout bounds the total duration of an outbound HTTP request,
	// including reading the response body.
	UpstreamTimeout time.Duration
	// MaxRequestBytes limits the size of incoming API request bodies.
	MaxRequestBytes int64
	// MaxResponseBodyBytes limits how much of an upstream response body is
	// read and stored.
	MaxResponseBodyBytes int64
	// AllowPrivateNetworks disables SSRF protection for loopback, private,
	// link-local and other non-public destinations.
	AllowPrivateNetworks bool

	// ShutdownTimeout bounds graceful shutdown.
	ShutdownTimeout time.Duration
}

// Load reads configuration from the environment and validates it.
func Load() (Config, error) {
	return load(os.Getenv)
}

func load(getenv func(string) string) (Config, error) {
	cfg := Config{
		HTTPAddr:             ":8080",
		DBMaxConns:           10,
		LogLevel:             slog.LevelInfo,
		UpstreamTimeout:      30 * time.Second,
		MaxRequestBytes:      2 << 20,  // 2 MiB
		MaxResponseBodyBytes: 10 << 20, // 10 MiB
		ShutdownTimeout:      15 * time.Second,
	}

	var errs []error
	p := parser{getenv: getenv, errs: &errs}

	p.str("HTTP_ADDR", &cfg.HTTPAddr)
	p.str("DATABASE_URL", &cfg.DatabaseURL)
	p.str("API_KEY", &cfg.APIKey)
	p.int32("DB_MAX_CONNS", &cfg.DBMaxConns, 1, 1000)
	p.level("LOG_LEVEL", &cfg.LogLevel)
	p.duration("UPSTREAM_TIMEOUT", &cfg.UpstreamTimeout, time.Second, 10*time.Minute)
	p.int64("MAX_REQUEST_BYTES", &cfg.MaxRequestBytes, 1<<10, 100<<20)
	p.int64("MAX_RESPONSE_BODY_BYTES", &cfg.MaxResponseBodyBytes, 1<<10, 100<<20)
	p.bool("ALLOW_PRIVATE_NETWORKS", &cfg.AllowPrivateNetworks)
	p.duration("SHUTDOWN_TIMEOUT", &cfg.ShutdownTimeout, time.Second, 5*time.Minute)

	if cfg.DatabaseURL == "" {
		errs = append(errs, errors.New("DATABASE_URL is required"))
	}
	if len(cfg.APIKey) < minAPIKeyLength {
		errs = append(errs, fmt.Errorf("API_KEY is required and must be at least %d characters", minAPIKeyLength))
	}

	if err := errors.Join(errs...); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

// parser collects validation errors while reading typed values.
type parser struct {
	getenv func(string) string
	errs   *[]error
}

func (p parser) lookup(key string) (string, bool) {
	v := strings.TrimSpace(p.getenv(key))
	return v, v != ""
}

func (p parser) fail(key, msg string) {
	*p.errs = append(*p.errs, fmt.Errorf("%s %s", key, msg))
}

func (p parser) str(key string, dst *string) {
	if v, ok := p.lookup(key); ok {
		*dst = v
	}
}

func (p parser) int64(key string, dst *int64, lo, hi int64) {
	v, ok := p.lookup(key)
	if !ok {
		return
	}
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil || n < lo || n > hi {
		p.fail(key, fmt.Sprintf("must be an integer between %d and %d", lo, hi))
		return
	}
	*dst = n
}

func (p parser) int32(key string, dst *int32, lo, hi int32) {
	n := int64(*dst)
	p.int64(key, &n, int64(lo), int64(hi))
	*dst = int32(n)
}

func (p parser) duration(key string, dst *time.Duration, lo, hi time.Duration) {
	v, ok := p.lookup(key)
	if !ok {
		return
	}
	d, err := time.ParseDuration(v)
	if err != nil || d < lo || d > hi {
		p.fail(key, fmt.Sprintf("must be a duration between %s and %s", lo, hi))
		return
	}
	*dst = d
}

func (p parser) bool(key string, dst *bool) {
	v, ok := p.lookup(key)
	if !ok {
		return
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		p.fail(key, "must be a boolean")
		return
	}
	*dst = b
}

func (p parser) level(key string, dst *slog.Level) {
	v, ok := p.lookup(key)
	if !ok {
		return
	}
	if err := dst.UnmarshalText([]byte(v)); err != nil {
		p.fail(key, "must be one of debug, info, warn, error")
	}
}
