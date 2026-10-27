package scheduler_test

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/reconvault/reconvault-dns-autoscan/internal/dnsscan"
	"github.com/reconvault/reconvault-dns-autoscan/internal/domainscan"
	"github.com/reconvault/reconvault-dns-autoscan/internal/netguard"
	"github.com/reconvault/reconvault-dns-autoscan/internal/resolver"
	"github.com/reconvault/reconvault-dns-autoscan/internal/scheduler"
	"github.com/reconvault/reconvault-dns-autoscan/internal/store"
	"github.com/reconvault/reconvault-dns-autoscan/internal/testdb"
	"github.com/reconvault/reconvault-dns-autoscan/internal/testdns"
)

func exampleZone(mx string) []string {
	return []string{
		testdns.SOA("example.test", 1),
		"example.test. 3600 IN NS ns1.example.test.",
		"ns1.example.test. 3600 IN A 127.0.0.1",
		"example.test. 300 IN A 192.0.2.10",
		"example.test. 3600 IN MX " + mx,
		`example.test. 300 IN TXT "v=spf1 -all"`,
		"www.example.test. 600 IN CNAME example.test.",
	}
}

var cymruZone = []string{
	testdns.SOA("asn.cymru.com", 1),
	`10.2.0.192.origin.asn.cymru.com. 300 IN TXT "64500 | 192.0.2.0/24 | ZZ | test | 2020-01-01"`,
	`AS64500.asn.cymru.com. 300 IN TXT "64500 | ZZ | test | 2020-01-01 | EXAMPLE-NET - Example Networks, ZZ"`,
}

func count(t *testing.T, pool *pgxpool.Pool, sql string, args ...any) int {
	t.Helper()
	var n int
	if err := pool.QueryRow(context.Background(), sql, args...).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestEndToEndActiveAndDaily(t *testing.T) {
	pool := testdb.New(t)
	srv := testdns.Start(t, exampleZone("10 mail.example.test."), cymruZone)
	ctx := context.Background()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))

	for _, h := range []string{"www.example.test", "www.example.test", "missing.example.test"} {
		if _, err := pool.Exec(ctx, "INSERT INTO http_artifacts (hostname) VALUES ($1)", h); err != nil {
			t.Fatal(err)
		}
	}

	policy := netguard.Policy{AllowPrivate: true}
	res := resolver.New(resolver.Options{Recursive: []string{srv.Addr}, Timeout: time.Second, AuthPort: srv.Port, Policy: policy})
	st := store.New(pool)
	sched := scheduler.New(st,
		// 192.0.2.0/24 is unroutable: the HTTP probe fails fast and WAF stays unknown.
		domainscan.NewScanner(res, domainscan.NewCymruASN(res), domainscan.NewWAFDetector(300*time.Millisecond, policy), log),
		dnsscan.New(res),
		scheduler.Options{BatchSize: 10, Concurrency: 4},
		log)

	// --- Active pass ---
	if err := sched.RunActive(ctx); err != nil {
		t.Fatal(err)
	}
	var (
		root    string
		asn     *int64
		asnName *string
		waf     *bool
	)
	if err := pool.QueryRow(ctx, "SELECT root_domain, asn, asn_name, waf FROM domains WHERE hostname = 'www.example.test'").Scan(&root, &asn, &asnName, &waf); err != nil {
		t.Fatal(err)
	}
	if root != "example.test" || asn == nil || *asn != 64500 || *asnName != "EXAMPLE-NET - Example Networks" || waf != nil {
		t.Fatalf("www domain: root=%s asn=%v name=%v waf=%v", root, asn, asnName, waf)
	}
	if err := pool.QueryRow(ctx, "SELECT asn, waf FROM domains WHERE hostname = 'missing.example.test'").Scan(&asn, &waf); err != nil || asn != nil || waf != nil {
		t.Fatalf("missing domain must have unknown ASN and WAF: asn=%v waf=%v err=%v", asn, waf, err)
	}
	if n := count(t, pool, "SELECT count(*) FROM domains"); n != 2 {
		t.Fatalf("domains = %d", n)
	}

	// Hostnames and the shared root domain were DNS-scanned.
	for _, h := range []string{"www.example.test", "missing.example.test", "example.test"} {
		if n := count(t, pool, "SELECT count(*) FROM dns_scan_state WHERE hostname = $1", h); n != len(dnsscan.RecordTypes) {
			t.Errorf("%s: %d scan states", h, n)
		}
	}
	var mx string
	var ttl int
	if err := pool.QueryRow(ctx, "SELECT record_value, ttl FROM dns_records_current WHERE hostname = 'example.test' AND record_type = 'MX'").Scan(&mx, &ttl); err != nil || mx != "10 mail.example.test" || ttl != 3600 {
		t.Fatalf("MX = %q ttl %d (%v)", mx, ttl, err)
	}
	if n := count(t, pool, "SELECT count(*) FROM dns_records_current WHERE hostname = 'www.example.test' AND record_type = 'CNAME' AND record_value = 'example.test'"); n != 1 {
		t.Errorf("www CNAME missing")
	}
	if n := count(t, pool, "SELECT count(*) FROM dns_scan_state WHERE hostname = 'missing.example.test' AND status = 'nxdomain'"); n != len(dnsscan.RecordTypes) {
		t.Errorf("missing host not recorded as NXDOMAIN: %d", n)
	}
	rowsBefore := count(t, pool, "SELECT count(*) FROM dns_records")

	// A second active pass finds nothing new to do.
	if err := sched.RunActive(ctx); err != nil {
		t.Fatal(err)
	}
	if n := count(t, pool, "SELECT count(*) FROM dns_records"); n != rowsBefore {
		t.Fatalf("idle active pass inserted %d rows", n-rowsBefore)
	}

	// --- Daily refresh after the MX record changed ---
	srv.Replace(t, exampleZone("20 mx2.example.test."))
	if err := sched.RunDaily(ctx); err != nil {
		t.Fatal(err)
	}
	// Exactly the changed MX: once for example.test and once for its alias
	// www.example.test, whose MX resolves through the CNAME. Unchanged
	// record sets are not re-inserted.
	if n := count(t, pool, "SELECT count(*) FROM dns_records"); n != rowsBefore+2 {
		t.Fatalf("daily refresh inserted %d rows, want 2", n-rowsBefore)
	}
	for _, h := range []string{"example.test", "www.example.test"} {
		if n := count(t, pool, "SELECT count(*) FROM dns_records WHERE hostname = $1 AND record_type = 'MX'", h); n != 2 {
			t.Fatalf("%s MX history rows = %d, want old and new", h, n)
		}
	}
	if err := pool.QueryRow(ctx, "SELECT record_value FROM dns_records_current WHERE hostname = 'example.test' AND record_type = 'MX'").Scan(&mx); err != nil || mx != "20 mx2.example.test" {
		t.Fatalf("current MX = %q (%v)", mx, err)
	}
	if last, err := st.LastCompleted(ctx, "daily"); err != nil || last == nil {
		t.Fatalf("daily completion not recorded: %v %v", last, err)
	}
}
