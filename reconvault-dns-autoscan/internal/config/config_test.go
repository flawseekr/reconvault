package config

import (
	"strings"
	"testing"
	"time"
)

func env(m map[string]string) func(string) string { return func(k string) string { return m[k] } }

func TestLoadDefaults(t *testing.T) {
	cfg, err := load(env(map[string]string{"DATABASE_URL": "postgres://x"}))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ActiveInterval != time.Minute || cfg.BatchSize != 100 || cfg.Concurrency != 16 || len(cfg.Resolvers) != 3 || cfg.AllowPrivateNetworks {
		t.Fatalf("defaults = %+v", cfg)
	}
}

func TestLoadResolvers(t *testing.T) {
	cfg, err := load(env(map[string]string{"DATABASE_URL": "x", "DNS_RESOLVERS": " 1.1.1.1 , [2606:4700::1111]:53, 9.9.9.9:5353 "}))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(cfg.Resolvers, ",") != "1.1.1.1:53,[2606:4700::1111]:53,9.9.9.9:5353" {
		t.Fatalf("resolvers = %v", cfg.Resolvers)
	}
}

func TestLoadErrors(t *testing.T) {
	for key, value := range map[string]string{
		"DNS_RESOLVERS":    "dns.google",
		"DNS_TIMEOUT":      "10ms",
		"SCAN_CONCURRENCY": "0",
		"BATCH_SIZE":       "x",
		"ACTIVE_INTERVAL":  "500ms",
		"LOG_LEVEL":        "loud",
	} {
		if _, err := load(env(map[string]string{"DATABASE_URL": "x", key: value})); err == nil || !strings.Contains(err.Error(), key) {
			t.Errorf("%s=%q: err = %v", key, value, err)
		}
	}
	if _, err := load(env(nil)); err == nil || !strings.Contains(err.Error(), "DATABASE_URL") {
		t.Errorf("missing DATABASE_URL: %v", err)
	}
}
