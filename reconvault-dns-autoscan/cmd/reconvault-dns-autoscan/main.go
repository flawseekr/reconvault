// Command reconvault-dns-autoscan runs the background DNS and domain
// scanner for ReconVault.
//
// Usage:
//
//	reconvault-dns-autoscan              run the service
//	reconvault-dns-autoscan healthcheck  probe the local /readyz endpoint (for Docker)
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/reconvault/reconvault-dns-autoscan/internal/config"
	"github.com/reconvault/reconvault-dns-autoscan/internal/database"
	"github.com/reconvault/reconvault-dns-autoscan/internal/dnsscan"
	"github.com/reconvault/reconvault-dns-autoscan/internal/domainscan"
	"github.com/reconvault/reconvault-dns-autoscan/internal/health"
	"github.com/reconvault/reconvault-dns-autoscan/internal/netguard"
	"github.com/reconvault/reconvault-dns-autoscan/internal/resolver"
	"github.com/reconvault/reconvault-dns-autoscan/internal/scheduler"
	"github.com/reconvault/reconvault-dns-autoscan/internal/store"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "healthcheck" {
		os.Exit(healthcheck())
	}
	if err := run(); err != nil {
		slog.Error("fatal", "error", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	log := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: cfg.LogLevel}))
	slog.SetDefault(log)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	connectCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	pool, err := database.Connect(connectCtx, cfg.DatabaseURL, cfg.DBMaxConns)
	cancel()
	if err != nil {
		return err
	}
	defer pool.Close()

	// http_artifacts is created by reconvault-api's migrations.
	if err := database.WaitForTable(ctx, pool, "http_artifacts", 5*time.Second, log); err != nil {
		return err
	}
	if err := database.Migrate(ctx, pool, log); err != nil {
		return fmt.Errorf("migrate: %w", err)
	}

	policy := netguard.Policy{AllowPrivate: cfg.AllowPrivateNetworks}
	if policy.AllowPrivate {
		log.Warn("SSRF protection disabled: scans may target private and loopback addresses")
	}
	res := resolver.New(resolver.Options{
		Recursive: cfg.Resolvers,
		Timeout:   cfg.DNSTimeout,
		Retries:   cfg.DNSRetries,
		Policy:    policy,
	})
	st := store.New(pool)
	sched := scheduler.New(
		st,
		domainscan.NewScanner(res, domainscan.NewCymruASN(res), domainscan.NewWAFDetector(cfg.HTTPProbeTimeout, policy), log),
		dnsscan.New(res),
		scheduler.Options{ActiveInterval: cfg.ActiveInterval, BatchSize: cfg.BatchSize, Concurrency: cfg.Concurrency},
		log,
	)

	srv := &http.Server{
		Addr:              cfg.HealthAddr,
		Handler:           health.Handler(st, log),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
	}
	srvErr := make(chan error, 1)
	go func() { srvErr <- srv.ListenAndServe() }()

	log.Info("dns autoscan started", "resolvers", cfg.Resolvers, "active_interval", cfg.ActiveInterval, "concurrency", cfg.Concurrency)
	done := make(chan struct{})
	go func() {
		sched.Run(ctx)
		close(done)
	}()

	select {
	case err := <-srvErr:
		stop()
		<-done
		return fmt.Errorf("health server: %w", err)
	case <-ctx.Done():
	}

	log.Info("shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer cancel()
	select {
	case <-done:
	case <-shutdownCtx.Done():
		log.Warn("scans did not stop before the shutdown timeout")
	}
	if err := srv.Shutdown(shutdownCtx); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

// healthcheck probes the readiness endpoint of the local process; the
// distroless image has no shell or curl.
func healthcheck() int {
	addr := os.Getenv("HEALTH_ADDR")
	if addr == "" {
		addr = ":8081"
	}
	_, port, err := net.SplitHostPort(addr)
	if err != nil {
		fmt.Fprintln(os.Stderr, "invalid HEALTH_ADDR:", err)
		return 1
	}
	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Get("http://" + net.JoinHostPort("127.0.0.1", port) + "/readyz")
	if err != nil {
		fmt.Fprintln(os.Stderr, "healthcheck:", err)
		return 1
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		fmt.Fprintln(os.Stderr, "healthcheck: status", resp.StatusCode)
		return 1
	}
	return 0
}
