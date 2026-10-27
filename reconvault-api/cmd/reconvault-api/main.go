// Command reconvault-api runs the ReconVault HTTP API server.
//
// Usage:
//
//	reconvault-api              start the server (applies migrations first)
//	reconvault-api healthcheck  probe the local /readyz endpoint (for Docker)
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

	"github.com/reconvault/reconvault-api/internal/artifact"
	"github.com/reconvault/reconvault-api/internal/config"
	"github.com/reconvault/reconvault-api/internal/database"
	"github.com/reconvault/reconvault-api/internal/fetcher"
	"github.com/reconvault/reconvault-api/internal/httpapi"
	"github.com/reconvault/reconvault-api/internal/postgres"
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

	if err := database.Migrate(ctx, pool, log); err != nil {
		return fmt.Errorf("migrate: %w", err)
	}

	if cfg.AllowPrivateNetworks {
		log.Warn("SSRF protection disabled: requests to private and loopback addresses are allowed")
	}
	svc := artifact.NewService(
		postgres.NewArtifactRepository(pool),
		fetcher.New(fetcher.Options{
			Timeout:              cfg.UpstreamTimeout,
			MaxBodyBytes:         cfg.MaxResponseBodyBytes,
			AllowPrivateNetworks: cfg.AllowPrivateNetworks,
		}),
	)

	srv := &http.Server{
		Addr: cfg.HTTPAddr,
		Handler: httpapi.NewHandler(svc, pool, log, httpapi.Options{
			APIKey:          cfg.APIKey,
			MaxRequestBytes: cfg.MaxRequestBytes,
			Recon:           postgres.NewReconRepository(pool),
		}),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		// Must outlast the upstream request plus the database write.
		WriteTimeout:   cfg.UpstreamTimeout + 30*time.Second,
		IdleTimeout:    120 * time.Second,
		MaxHeaderBytes: 1 << 20,
		ErrorLog:       slog.NewLogLogger(log.Handler(), slog.LevelWarn),
		// Request contexts intentionally do not derive from ctx: on SIGTERM,
		// Shutdown drains in-flight requests instead of cancelling them.
	}

	errCh := make(chan error, 1)
	go func() {
		log.Info("server listening", "addr", cfg.HTTPAddr)
		errCh <- srv.ListenAndServe()
	}()

	select {
	case err := <-errCh:
		return fmt.Errorf("listen: %w", err)
	case <-ctx.Done():
	}

	log.Info("shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("shutdown: %w", err)
	}
	if err := <-errCh; !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

// healthcheck queries the readiness endpoint of the local server. It exists
// because the distroless runtime image has no shell, curl or wget.
func healthcheck() int {
	addr := os.Getenv("HTTP_ADDR")
	if addr == "" {
		addr = ":8080"
	}
	_, port, err := net.SplitHostPort(addr)
	if err != nil {
		fmt.Fprintln(os.Stderr, "invalid HTTP_ADDR:", err)
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
