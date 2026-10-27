// Package testdb provides an isolated, migrated PostgreSQL schema for
// integration tests. Tests are skipped unless TEST_DATABASE_URL is set.
package testdb

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/reconvault/reconvault-dns-autoscan/internal/database"
)

// httpArtifacts mirrors the columns of reconvault-api's table that this
// service reads.
const httpArtifacts = `CREATE TABLE http_artifacts (
	id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
	http_method VARCHAR(16) NOT NULL DEFAULT 'GET',
	hostname TEXT NOT NULL,
	endpoint_path TEXT NOT NULL DEFAULT '/',
	full_url TEXT NOT NULL DEFAULT '',
	requested_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
)`

// New returns a pool whose search_path is a fresh schema containing
// http_artifacts and this service's migrated tables.
func New(t *testing.T) *pgxpool.Pool {
	t.Helper()
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL not set; skipping integration test")
	}
	ctx := context.Background()
	admin, err := database.Connect(ctx, url, 2)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	schema := fmt.Sprintf("autoscan_test_%d", time.Now().UnixNano())
	if _, err := admin.Exec(ctx, "CREATE SCHEMA "+schema); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		c, err := database.Connect(context.Background(), url, 1)
		if err == nil {
			_, _ = c.Exec(context.Background(), "DROP SCHEMA "+schema+" CASCADE")
			c.Close()
		}
	})
	sep := "?"
	if strings.Contains(url, "?") {
		sep = "&"
	}
	pool, err := database.Connect(ctx, url+sep+"search_path="+schema, 8)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	if _, err := pool.Exec(ctx, httpArtifacts); err != nil {
		t.Fatal(err)
	}
	if err := database.Migrate(ctx, pool, slog.New(slog.NewTextHandler(io.Discard, nil))); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return pool
}
