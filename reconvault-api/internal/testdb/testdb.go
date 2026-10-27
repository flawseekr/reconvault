// Package testdb provides a migrated PostgreSQL pool for integration tests.
// Tests using it are skipped unless TEST_DATABASE_URL is set.
package testdb

import (
	"context"
	"io"
	"log/slog"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/reconvault/reconvault-api/internal/database"
)

// New connects to TEST_DATABASE_URL, applies migrations and empties
// http_artifacts. Packages using it must not run in parallel with each
// other (go test -p 1).
func New(t *testing.T) *pgxpool.Pool {
	t.Helper()
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL not set; skipping integration test")
	}
	ctx := context.Background()
	pool, err := database.Connect(ctx, url, 5)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(pool.Close)
	if err := database.Migrate(ctx, pool, slog.New(slog.NewTextHandler(io.Discard, nil))); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if _, err := pool.Exec(ctx, "TRUNCATE http_artifacts"); err != nil {
		t.Fatalf("truncate: %v", err)
	}
	return pool
}
