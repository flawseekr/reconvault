package database

import (
	"context"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// isolatedPool connects with search_path set to a fresh schema so these
// tests never touch the real schema_migrations table.
func isolatedPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL not set; skipping integration test")
	}
	ctx := context.Background()
	admin, err := Connect(ctx, url, 2)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	schema := fmt.Sprintf("migrate_test_%d", time.Now().UnixNano())
	if _, err := admin.Exec(ctx, "CREATE SCHEMA "+schema); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		c, err := Connect(context.Background(), url, 1)
		if err == nil {
			c.Exec(context.Background(), "DROP SCHEMA "+schema+" CASCADE")
			c.Close()
		}
	})
	sep := "?"
	if strings.Contains(url, "?") {
		sep = "&"
	}
	// public stays on the path so a database-wide pg_trgm installed there
	// by an earlier run remains visible; new objects go into schema.
	pool, err := Connect(ctx, url+sep+"search_path="+schema+",public", 2)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	return pool
}

var discard = slog.New(slog.NewTextHandler(io.Discard, nil))

func versions(t *testing.T, pool *pgxpool.Pool) string {
	t.Helper()
	var v string
	err := pool.QueryRow(context.Background(),
		"SELECT coalesce(string_agg(version, ',' ORDER BY version), '') FROM schema_migrations").Scan(&v)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func tableExists(t *testing.T, pool *pgxpool.Pool, name string) bool {
	t.Helper()
	var ok bool
	if err := pool.QueryRow(context.Background(), "SELECT to_regclass($1) IS NOT NULL", name).Scan(&ok); err != nil {
		t.Fatal(err)
	}
	return ok
}

func TestMigrateFailedMigrationRollsBack(t *testing.T) {
	pool := isolatedPool(t)
	ctx := context.Background()

	fsys := fstest.MapFS{
		"0001_a.sql": {Data: []byte("CREATE TABLE a (id int);")},
		// Fails halfway: b is created, then the statement errors.
		"0002_b.sql": {Data: []byte("CREATE TABLE b (id int); SELECT 1/0;")},
		"0003_c.sql": {Data: []byte("CREATE TABLE c (id int);")},
	}
	if err := migrate(ctx, pool, discard, fsys); err == nil || !strings.Contains(err.Error(), "0002_b") {
		t.Fatalf("err = %v, want failure in 0002_b", err)
	}
	if got := versions(t, pool); got != "0001_a" {
		t.Errorf("applied = %q, want only 0001_a", got)
	}
	if !tableExists(t, pool, "a") || tableExists(t, pool, "b") || tableExists(t, pool, "c") {
		t.Error("failed migration must roll back fully and stop later migrations")
	}

	// Once fixed, the remaining migrations apply and earlier ones are skipped.
	fsys["0002_b.sql"] = &fstest.MapFile{Data: []byte("CREATE TABLE b (id int);")}
	if err := migrate(ctx, pool, discard, fsys); err != nil {
		t.Fatal(err)
	}
	if got := versions(t, pool); got != "0001_a,0002_b,0003_c" {
		t.Errorf("applied = %q", got)
	}
	if err := migrate(ctx, pool, discard, fsys); err != nil {
		t.Fatalf("re-run must be a no-op: %v", err)
	}
}

func TestMigrateConcurrentRunsAreSerialized(t *testing.T) {
	pool := isolatedPool(t)
	fsys := fstest.MapFS{
		"0001_slow.sql": {Data: []byte("SELECT pg_sleep(0.3); CREATE TABLE slow (id int);")},
	}
	errs := make(chan error, 3)
	for range 3 {
		go func() { errs <- migrate(context.Background(), pool, discard, fsys) }()
	}
	for range 3 {
		if err := <-errs; err != nil {
			t.Fatalf("concurrent migrate: %v", err)
		}
	}
	if got := versions(t, pool); got != "0001_slow" {
		t.Errorf("applied = %q", got)
	}
}

func TestEmbeddedMigrationsApply(t *testing.T) {
	pool := isolatedPool(t)
	ctx := context.Background()
	// pg_trgm is database-wide; the embedded migrations create the
	// table, indexes and search function inside the isolated schema.
	if err := Migrate(ctx, pool, discard); err != nil {
		t.Fatal(err)
	}
	want := "0001_create_http_artifacts,0002_create_http_artifacts_indexes,0003_create_http_artifacts_search," +
		"0004_rename_response_body_to_response"
	if got := versions(t, pool); got != want {
		t.Errorf("applied = %q", got)
	}
}

// TestMigrationKeepsExistingResponses upgrades a database created before
// 0004 and checks the stored body survives the rename and stays searchable.
func TestMigrationKeepsExistingResponses(t *testing.T) {
	pool := isolatedPool(t)
	ctx := context.Background()
	sub, err := fs.Sub(migrationFiles, "migrations")
	if err != nil {
		t.Fatal(err)
	}
	old := fstest.MapFS{}
	for _, name := range []string{"0001_create_http_artifacts.sql", "0002_create_http_artifacts_indexes.sql", "0003_create_http_artifacts_search.sql"} {
		b, err := fs.ReadFile(sub, name)
		if err != nil {
			t.Fatal(err)
		}
		old[name] = &fstest.MapFile{Data: b}
	}
	if err := migrate(ctx, pool, discard, old); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO http_artifacts (http_method, hostname, endpoint_path, full_url, response_body)
		VALUES ('GET', 'a.com', '/', 'https://a.com/', 'legacy-secret-body')`); err != nil {
		t.Fatal(err)
	}

	if err := Migrate(ctx, pool, discard); err != nil {
		t.Fatal(err)
	}
	var response string
	if err := pool.QueryRow(ctx, "SELECT response FROM http_artifacts").Scan(&response); err != nil || response != "legacy-secret-body" {
		t.Fatalf("response = %q (%v)", response, err)
	}
	var n int
	err = pool.QueryRow(ctx, `SELECT count(*) FROM http_artifacts WHERE http_artifact_search_document(
		id, http_method, hostname, endpoint_path, full_url, requested_at, response_status_code,
		response_content_type, response_body_size_bytes, request_headers, request_body, response) ILIKE '%legacy-secret%'`).Scan(&n)
	if err != nil || n != 1 {
		t.Fatalf("search after rename: n=%d err=%v", n, err)
	}
}

func TestConnectErrors(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := Connect(ctx, "postgres://%zz", 1); err == nil {
		t.Error("expected parse error")
	}
	if _, err := Connect(ctx, "postgres://u:p@127.0.0.1:1/db?connect_timeout=1", 1); err == nil {
		t.Error("expected ping error")
	}
}
