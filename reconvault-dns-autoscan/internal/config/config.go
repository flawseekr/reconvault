// Package config loads and validates the service configuration from
// environment variables.
package config

import (
	"errors"
	"fmt"
	"log/slog"
	"net"
	"os"
	"strconv"
	"strings"
	"time"
)

// Config holds the runtime configuration.
type Config struct {
	DatabaseURL string
	DBMaxConns  int32
	LogLevel    slog.Level
	// HealthAddr serves /healthz and /readyz.
	HealthAddr string

	// Resolvers are recursive DNS servers (host:port).
	Resolvers  []string
	DNSTimeout time.Duration
	DNSRetries int

	// ActiveInterval is the pause between active passes.
	ActiveInterval time.Duration
	// BatchSize bounds the hostnames claimed per database query.
	BatchSize int
	// Concurrency bounds hostnames scanned in parallel.
	Concurrency int
	// HTTPProbeTimeout bounds the WAF fingerprinting request.
	HTTPProbeTimeout time.Duration
	// AllowPrivateNetworks permits scanning non-public addresses.
	AllowPrivateNetworks bool

	ShutdownTimeout time.Duration
}

// Load reads the configuration from the environment.
func Load() (Config, error) { return load(os.Getenv) }

func load(getenv func(string) string) (Config, error) {
	cfg := Config{
		DBMaxConns:       10,
		LogLevel:         slog.LevelInfo,
		HealthAddr:       ":8081",
		Resolvers:        []string{"1.1.1.1:53", "8.8.8.8:53", "9.9.9.9:53"},
		DNSTimeout:       5 * time.Second,
		DNSRetries:       2,
		ActiveInterval:   time.Minute,
		BatchSize:        100,
		Concurrency:      16,
		HTTPProbeTimeout: 10 * time.Second,
		ShutdownTimeout:  30 * time.Second,
	}
	var errs []error
	p := parser{getenv: getenv, errs: &errs}

	p.str("DATABASE_URL", &cfg.DatabaseURL)
	p.str("HEALTH_ADDR", &cfg.HealthAddr)
	p.int("DB_MAX_CONNS", func(n int) { cfg.DBMaxConns = int32(n) }, 1, 1000)
	p.level("LOG_LEVEL", &cfg.LogLevel)
	p.resolvers("DNS_RESOLVERS", &cfg.Resolvers)
	p.duration("DNS_TIMEOUT", &cfg.DNSTimeout, 100*time.Millisecond, time.Minute)
	p.int("DNS_RETRIES", func(n int) { cfg.DNSRetries = n }, 0, 10)
	p.duration("ACTIVE_INTERVAL", &cfg.ActiveInterval, time.Second, 24*time.Hour)
	p.int("BATCH_SIZE", func(n int) { cfg.BatchSize = n }, 1, 10000)
	p.int("SCAN_CONCURRENCY", func(n int) { cfg.Concurrency = n }, 1, 512)
	p.duration("HTTP_PROBE_TIMEOUT", &cfg.HTTPProbeTimeout, time.Second, 2*time.Minute)
	p.bool("ALLOW_PRIVATE_NETWORKS", &cfg.AllowPrivateNetworks)
	p.duration("SHUTDOWN_TIMEOUT", &cfg.ShutdownTimeout, time.Second, 10*time.Minute)

	if cfg.DatabaseURL == "" {
		errs = append(errs, errors.New("DATABASE_URL is required"))
	}
	if err := errors.Join(errs...); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

type parser struct {
	getenv func(string) string
	errs   *[]error
}

func (p parser) lookup(key string) (string, bool) {
	v := strings.TrimSpace(p.getenv(key))
	return v, v != ""
}

func (p parser) fail(key, msg string) { *p.errs = append(*p.errs, fmt.Errorf("%s %s", key, msg)) }

func (p parser) str(key string, dst *string) {
	if v, ok := p.lookup(key); ok {
		*dst = v
	}
}

func (p parser) int(key string, set func(int), lo, hi int) {
	v, ok := p.lookup(key)
	if !ok {
		return
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < lo || n > hi {
		p.fail(key, fmt.Sprintf("must be an integer between %d and %d", lo, hi))
		return
	}
	set(n)
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
	if v, ok := p.lookup(key); ok {
		if err := dst.UnmarshalText([]byte(v)); err != nil {
			p.fail(key, "must be one of debug, info, warn, error")
		}
	}
}

// resolvers parses a comma-separated host[:port] list; port defaults to 53.
func (p parser) resolvers(key string, dst *[]string) {
	v, ok := p.lookup(key)
	if !ok {
		return
	}
	var out []string
	for _, item := range strings.Split(v, ",") {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		if _, _, err := net.SplitHostPort(item); err != nil {
			item = net.JoinHostPort(item, "53")
		}
		host, port, err := net.SplitHostPort(item)
		if err != nil || net.ParseIP(host) == nil {
			p.fail(key, fmt.Sprintf("entry %q must be an IP address with an optional port", item))
			return
		}
		if n, err := strconv.Atoi(port); err != nil || n < 1 || n > 65535 {
			p.fail(key, fmt.Sprintf("entry %q has an invalid port", item))
			return
		}
		out = append(out, item)
	}
	if len(out) == 0 {
		p.fail(key, "must list at least one resolver")
		return
	}
	*dst = out
}
