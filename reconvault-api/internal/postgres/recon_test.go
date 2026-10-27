package postgres

import (
	"context"
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/reconvault/reconvault-api/internal/database"
	"github.com/reconvault/reconvault-api/internal/recon"
	"github.com/reconvault/reconvault-api/internal/testdb"
)

func reconSetup(t *testing.T) (*ReconRepository, *pgxpool.Pool) {
	pool := testdb.New(t)
	testdb.WithReconTables(t, pool)
	return NewReconRepository(pool), pool
}

func mustExec(t *testing.T, pool *pgxpool.Pool, sql string, args ...any) {
	t.Helper()
	if _, err := pool.Exec(context.Background(), sql, args...); err != nil {
		t.Fatal(err)
	}
}

func TestReconDomain(t *testing.T) {
	repo, pool := reconSetup(t)
	ctx := context.Background()
	mustExec(t, pool, `INSERT INTO domains (hostname, root_domain, asn, asn_name, waf, updated_at) VALUES
		('www.example.co.uk', 'example.co.uk', 13335, 'CLOUDFLARENET', true, '2026-10-27T12:00:00.123456Z'),
		('bare.test', 'bare.test', NULL, NULL, NULL, NOW())`)

	d, err := repo.Domain(ctx, "www.example.co.uk")
	if err != nil {
		t.Fatal(err)
	}
	if d.RootDomain != "example.co.uk" || *d.ASN != 13335 || *d.ASNName != "CLOUDFLARENET" || !*d.WAF ||
		!d.UpdatedAt.Equal(time.Date(2026, 10, 27, 12, 0, 0, 123456000, time.UTC)) || d.UpdatedAt.Location() != time.UTC {
		t.Errorf("domain = %+v", d)
	}
	bare, err := repo.Domain(ctx, "bare.test")
	if err != nil || bare.ASN != nil || bare.ASNName != nil || bare.WAF != nil {
		t.Errorf("NULLs must stay nil: %+v, %v", bare, err)
	}
	if _, err := repo.Domain(ctx, "missing.test"); !errors.Is(err, recon.ErrNotFound) {
		t.Errorf("missing: %v", err)
	}
}

func TestReconDNSHistoryIsCompleteAndNewestFirst(t *testing.T) {
	repo, pool := reconSetup(t)
	base := time.Date(2026, 10, 27, 0, 0, 0, 0, time.UTC)
	// Three versions of A, one of MX, plus another host that must not leak in.
	for i := range 3 {
		mustExec(t, pool, "INSERT INTO dns_records (hostname, record_type, record_value, ttl, updated_at) VALUES ('h.test', 'A', $1, 300, $2)",
			fmt.Sprintf("192.0.2.%d", i), base.Add(time.Duration(i)*time.Hour))
	}
	mustExec(t, pool, "INSERT INTO dns_records (hostname, record_type, record_value, ttl, updated_at) VALUES ('h.test', 'MX', '10 mx.h.test', NULL, $1)", base.Add(time.Hour))
	mustExec(t, pool, "INSERT INTO dns_records (hostname, record_type, record_value, ttl) VALUES ('other.test', 'A', '198.51.100.1', 60)")
	// More rows than any listing limit, to prove there is none.
	for i := range 150 {
		mustExec(t, pool, "INSERT INTO dns_records (hostname, record_type, record_value, ttl, updated_at) VALUES ('h.test', 'TXT', $1, 60, $2)",
			fmt.Sprintf("t%03d", i), base.Add(-time.Hour))
	}

	got, err := repo.DNSHistory(context.Background(), "h.test")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 154 {
		t.Fatalf("rows = %d, want all 154", len(got))
	}
	for i := 1; i < len(got); i++ {
		if got[i].UpdatedAt.After(got[i-1].UpdatedAt) {
			t.Fatalf("not newest first at %d: %v after %v", i, got[i].UpdatedAt, got[i-1].UpdatedAt)
		}
	}
	if got[0].RecordValue != "192.0.2.2" || got[1].RecordType != "A" || got[2].RecordType != "MX" || got[2].TTL != nil {
		t.Errorf("head = %+v %+v %+v", got[0], got[1], got[2])
	}
	if *got[0].TTL != 300 || got[0].Hostname != "h.test" {
		t.Errorf("first = %+v", got[0])
	}
	if empty, err := repo.DNSHistory(context.Background(), "nothing.test"); err != nil || len(empty) != 0 {
		t.Errorf("unknown host: %v, %v", empty, err)
	}
}

func TestReconSubdomainsAndRootDomains(t *testing.T) {
	repo, pool := reconSetup(t)
	ctx := context.Background()
	mustExec(t, pool, `INSERT INTO domains (hostname, root_domain) VALUES
		('www.example.com', 'example.com'), ('api.example.com', 'example.com'), ('example.com', 'example.com'),
		('a.b.example.co.uk', 'example.co.uk'), ('solo.test', 'solo.test')`)

	subs, err := repo.Subdomains(ctx, "example.com")
	if err != nil {
		t.Fatal(err)
	}
	names := make([]string, len(subs))
	for i, s := range subs {
		names[i] = s.Hostname
	}
	if !slices.Equal(names, []string{"api.example.com", "example.com", "www.example.com"}) {
		t.Errorf("subdomains = %v", names)
	}
	if none, err := repo.Subdomains(ctx, "unknown.test"); err != nil || len(none) != 0 {
		t.Errorf("unknown root: %v, %v", none, err)
	}

	roots, err := repo.RootDomains(ctx, 100)
	if err != nil {
		t.Fatal(err)
	}
	want := []recon.RootDomain{
		{RootDomain: "example.com", HostnameCount: 3},
		{RootDomain: "example.co.uk", HostnameCount: 1},
		{RootDomain: "solo.test", HostnameCount: 1},
	}
	if !slices.Equal(roots, want) {
		t.Errorf("root domains = %+v", roots)
	}
	if top, _ := repo.RootDomains(ctx, 1); len(top) != 1 || top[0].RootDomain != "example.com" {
		t.Errorf("limit: %+v", top)
	}
	// Totals match the subdomain listing.
	for _, r := range roots {
		subs, _ := repo.Subdomains(ctx, r.RootDomain)
		if int64(len(subs)) != r.HostnameCount {
			t.Errorf("%s: count %d, listing %d", r.RootDomain, r.HostnameCount, len(subs))
		}
	}
}

// Before the autoscan service creates its tables, the API reports "no
// data" instead of failing.
func TestReconWithoutAutoscanTables(t *testing.T) {
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL not set; skipping integration test")
	}
	ctx := context.Background()
	admin, err := database.Connect(ctx, url, 1)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	schema := fmt.Sprintf("recon_empty_%d", time.Now().UnixNano())
	mustExec(t, admin, "CREATE SCHEMA "+schema)
	defer admin.Exec(context.Background(), "DROP SCHEMA "+schema+" CASCADE")
	sep := "?"
	if strings.Contains(url, "?") {
		sep = "&"
	}
	pool, err := database.Connect(ctx, url+sep+"search_path="+schema, 1)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	repo := NewReconRepository(pool)

	if _, err := repo.Domain(ctx, "a.test"); !errors.Is(err, recon.ErrNotFound) {
		t.Errorf("domain: %v", err)
	}
	if recs, err := repo.DNSHistory(ctx, "a.test"); err != nil || len(recs) != 0 {
		t.Errorf("dns: %v %v", recs, err)
	}
	if subs, err := repo.Subdomains(ctx, "a.test"); err != nil || len(subs) != 0 {
		t.Errorf("subdomains: %v %v", subs, err)
	}
	if roots, err := repo.RootDomains(ctx, 10); err != nil || len(roots) != 0 {
		t.Errorf("roots: %v %v", roots, err)
	}
}
