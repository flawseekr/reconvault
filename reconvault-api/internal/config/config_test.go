package config

import (
	"log/slog"
	"strings"
	"testing"
	"time"
)

const validKey = "0123456789abcdef0123456789abcdef"

func env(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func TestLoadDefaults(t *testing.T) {
	cfg, err := load(env(map[string]string{"DATABASE_URL": "postgres://x", "API_KEY": validKey}))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.HTTPAddr != ":8080" || cfg.DBMaxConns != 10 || cfg.LogLevel != slog.LevelInfo ||
		cfg.UpstreamTimeout != 30*time.Second || cfg.MaxRequestBytes != 2<<20 ||
		cfg.MaxResponseBodyBytes != 10<<20 || cfg.AllowPrivateNetworks || cfg.ShutdownTimeout != 15*time.Second {
		t.Fatalf("unexpected defaults: %+v", cfg)
	}
}

func TestLoadOverrides(t *testing.T) {
	cfg, err := load(env(map[string]string{
		"DATABASE_URL":            "postgres://x",
		"API_KEY":                 validKey,
		"HTTP_ADDR":               ":9090",
		"DB_MAX_CONNS":            "25",
		"LOG_LEVEL":               "debug",
		"UPSTREAM_TIMEOUT":        "5s",
		"MAX_REQUEST_BYTES":       "4096",
		"MAX_RESPONSE_BODY_BYTES": "1048576",
		"ALLOW_PRIVATE_NETWORKS":  "true",
		"SHUTDOWN_TIMEOUT":        " 2s ",
	}))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.HTTPAddr != ":9090" || cfg.DBMaxConns != 25 || cfg.LogLevel != slog.LevelDebug ||
		cfg.UpstreamTimeout != 5*time.Second || cfg.MaxRequestBytes != 4096 ||
		cfg.MaxResponseBodyBytes != 1<<20 || !cfg.AllowPrivateNetworks || cfg.ShutdownTimeout != 2*time.Second {
		t.Fatalf("overrides not applied: %+v", cfg)
	}
}

func TestLoadErrors(t *testing.T) {
	base := map[string]string{"DATABASE_URL": "postgres://x", "API_KEY": validKey}
	tests := []struct {
		name, key, value, want string
	}{
		{"missing database url", "DATABASE_URL", "", "DATABASE_URL is required"},
		{"missing api key", "API_KEY", "", "API_KEY"},
		{"short api key", "API_KEY", "short", "at least 32"},
		{"api key one below minimum", "API_KEY", validKey[:31], "at least 32"},
		{"bad int", "DB_MAX_CONNS", "abc", "DB_MAX_CONNS"},
		{"int below range", "DB_MAX_CONNS", "0", "DB_MAX_CONNS"},
		{"int above range", "DB_MAX_CONNS", "1001", "DB_MAX_CONNS"},
		{"bad duration", "UPSTREAM_TIMEOUT", "soon", "UPSTREAM_TIMEOUT"},
		{"duration below range", "UPSTREAM_TIMEOUT", "500ms", "UPSTREAM_TIMEOUT"},
		{"bytes below range", "MAX_RESPONSE_BODY_BYTES", "10", "MAX_RESPONSE_BODY_BYTES"},
		{"bad bool", "ALLOW_PRIVATE_NETWORKS", "maybe", "ALLOW_PRIVATE_NETWORKS"},
		{"bad level", "LOG_LEVEL", "loud", "LOG_LEVEL"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := map[string]string{}
			for k, v := range base {
				m[k] = v
			}
			m[tt.key] = tt.value
			_, err := load(env(m))
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("want error containing %q, got %v", tt.want, err)
			}
		})
	}
}

func TestLoadReportsAllErrors(t *testing.T) {
	_, err := load(env(map[string]string{"DB_MAX_CONNS": "x"}))
	if err == nil {
		t.Fatal("expected error")
	}
	for _, want := range []string{"DATABASE_URL", "API_KEY", "DB_MAX_CONNS"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not mention %s", err, want)
		}
	}
}

func TestLoadAPIKeyAtMinimumLength(t *testing.T) {
	if _, err := load(env(map[string]string{"DATABASE_URL": "postgres://x", "API_KEY": validKey})); err != nil {
		t.Fatalf("32-character key rejected: %v", err)
	}
}
