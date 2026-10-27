package store_test

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/reconvault/reconvault-dns-autoscan/internal/dnsscan"
	"github.com/reconvault/reconvault-dns-autoscan/internal/domainscan"
	"github.com/reconvault/reconvault-dns-autoscan/internal/store"
	"github.com/reconvault/reconvault-dns-autoscan/internal/testdb"
)

func setup(t *testing.T) (*store.Store, *pgxpool.Pool) {
	pool := testdb.New(t)
	return store.New(pool), pool
}

func exec(t *testing.T, pool *pgxpool.Pool, sql string, args ...any) {
	t.Helper()
	if _, err := pool.Exec(context.Background(), sql, args...); err != nil {
		t.Fatal(err)
	}
}

func ptr[T any](v T) *T { return &v }

func TestPendingDomainHosts(t *testing.T) {
	s, pool := setup(t)
	for _, h := range []string{"b.test", "a.test", "b.test", "c.test"} {
		exec(t, pool, "INSERT INTO http_artifacts (hostname) VALUES ($1)", h)
	}
	exec(t, pool, "INSERT INTO domains (hostname, root_domain) VALUES ('b.test', 'b.test')")
	got, err := s.PendingDomainHosts(context.Background(), 10)
	if err != nil || !slices.Equal(got, []string{"a.test", "c.test"}) {
		t.Fatalf("got %v, %v", got, err)
	}
	if got, _ := s.PendingDomainHosts(context.Background(), 1); !slices.Equal(got, []string{"a.test"}) {
		t.Errorf("limit: %v", got)
	}
}

type domainRow struct {
	root    string
	asn     *int64
	asnName *string
	waf     *bool
}

func readDomain(t *testing.T, pool *pgxpool.Pool, host string) domainRow {
	t.Helper()
	var d domainRow
	err := pool.QueryRow(context.Background(), "SELECT root_domain, asn, asn_name, waf FROM domains WHERE hostname = $1", host).
		Scan(&d.root, &d.asn, &d.asnName, &d.waf)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func TestUpsertDomainKeepsVerifiedValues(t *testing.T) {
	s, pool := setup(t)
	ctx := context.Background()
	full := domainscan.Domain{Hostname: "www.example.co.uk", RootDomain: "example.co.uk", ASN: ptr(int64(13335)), ASNName: ptr("CLOUDFLARENET"), WAF: ptr(true)}
	if err := s.UpsertDomain(ctx, full); err != nil {
		t.Fatal(err)
	}
	// An inconclusive rescan must not erase verified data.
	if err := s.UpsertDomain(ctx, domainscan.Domain{Hostname: "www.example.co.uk", RootDomain: "example.co.uk"}); err != nil {
		t.Fatal(err)
	}
	d := readDomain(t, pool, "www.example.co.uk")
	if d.root != "example.co.uk" || d.asn == nil || *d.asn != 13335 || *d.asnName != "CLOUDFLARENET" || d.waf == nil || !*d.waf {
		t.Fatalf("after inconclusive rescan: %+v", d)
	}
	// A conclusive rescan replaces number and name together.
	if err := s.UpsertDomain(ctx, domainscan.Domain{Hostname: "www.example.co.uk", RootDomain: "example.co.uk", ASN: ptr(int64(15169)), ASNName: ptr("GOOGLE"), WAF: ptr(false)}); err != nil {
		t.Fatal(err)
	}
	d = readDomain(t, pool, "www.example.co.uk")
	if *d.asn != 15169 || *d.asnName != "GOOGLE" || *d.waf {
		t.Fatalf("after conclusive rescan: %+v", d)
	}
	var n int
	_ = pool.QueryRow(ctx, "SELECT count(*) FROM domains").Scan(&n)
	if n != 1 {
		t.Errorf("rows = %d", n)
	}
}

func TestPendingDNSTargets(t *testing.T) {
	s, pool := setup(t)
	ctx := context.Background()
	exec(t, pool, `INSERT INTO domains (hostname, root_domain) VALUES
		('www.example.test', 'example.test'), ('api.example.test', 'example.test'), ('other.test', 'other.test')`)
	got, err := s.PendingDNSTargets(ctx, 10)
	if err != nil || !slices.Equal(got, []string{"api.example.test", "example.test", "other.test", "www.example.test"}) {
		t.Fatalf("got %v, %v", got, err)
	}
	// Any recorded outcome, even NXDOMAIN, marks a target as scanned.
	if _, err := s.ApplyDNSOutcomes(ctx, "example.test", []dnsscan.Outcome{{Type: "A", Status: dnsscan.StatusNXDomain}}, time.Now()); err != nil {
		t.Fatal(err)
	}
	got, _ = s.PendingDNSTargets(ctx, 10)
	if slices.Contains(got, "example.test") || len(got) != 3 {
		t.Fatalf("after scan: %v", got)
	}
	scanned, err := s.ScannedDNSTargets(ctx, "", 10)
	if err != nil || !slices.Equal(scanned, []string{"example.test"}) {
		t.Fatalf("scanned: %v, %v", scanned, err)
	}
}

func found(values ...string) dnsscan.Outcome {
	o := dnsscan.Outcome{Type: "A", Status: dnsscan.StatusFound}
	for _, v := range values {
		o.Records = append(o.Records, dnsscan.Record{Type: "A", Value: v, TTL: 300})
	}
	return o
}

func count(t *testing.T, pool *pgxpool.Pool, sql string, args ...any) int {
	t.Helper()
	var n int
	if err := pool.QueryRow(context.Background(), sql, args...).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func current(t *testing.T, pool *pgxpool.Pool) []string {
	t.Helper()
	rows, err := pool.Query(context.Background(), "SELECT record_value FROM dns_records_current WHERE hostname = 'h.test' AND record_type = 'A' ORDER BY 1")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var v string
		_ = rows.Scan(&v)
		out = append(out, v)
	}
	return out
}

func TestDNSVersioning(t *testing.T) {
	s, pool := setup(t)
	ctx := context.Background()
	t0 := time.Date(2026, 10, 28, 0, 0, 0, 123456789, time.UTC)
	apply := func(step int, o dnsscan.Outcome) store.ApplyResult {
		t.Helper()
		res, err := s.ApplyDNSOutcomes(ctx, "h.test", []dnsscan.Outcome{o}, t0.Add(time.Duration(step)*time.Hour))
		if err != nil {
			t.Fatalf("step %d: %v", step, err)
		}
		return res
	}
	rows := func() int { return count(t, pool, "SELECT count(*) FROM dns_records WHERE hostname = 'h.test'") }

	// 1. First observation inserts a version.
	if r := apply(1, found("192.0.2.2", "192.0.2.1")); r.NewVersions != 1 || rows() != 2 {
		t.Fatalf("step 1: %+v rows=%d", r, rows())
	}
	// 2. Same set (different TTL) inserts nothing.
	same := found("192.0.2.1", "192.0.2.2")
	same.Records[0].TTL = 60
	if r := apply(2, same); r.Unchanged != 1 || rows() != 2 {
		t.Fatalf("step 2: %+v rows=%d", r, rows())
	}
	// 3. A changed set is inserted as a new version; history is kept.
	if r := apply(3, found("192.0.2.1", "192.0.2.3")); r.NewVersions != 1 || rows() != 4 {
		t.Fatalf("step 3: %+v rows=%d", r, rows())
	}
	if c := current(t, pool); !slices.Equal(c, []string{"192.0.2.1", "192.0.2.3"}) {
		t.Fatalf("current after change: %v", c)
	}
	// 4. A failed lookup keeps the current records.
	if r := apply(4, dnsscan.Outcome{Type: "A", Status: dnsscan.StatusError, Err: errors.New("i/o timeout")}); r.Failed != 1 || rows() != 4 {
		t.Fatalf("step 4: %+v", r)
	}
	if c := current(t, pool); !slices.Equal(c, []string{"192.0.2.1", "192.0.2.3"}) {
		t.Fatalf("records lost after a failed lookup: %v", c)
	}
	var status, lastErr string
	var failures int
	_ = pool.QueryRow(ctx, "SELECT status, last_error, consecutive_failures FROM dns_scan_state WHERE hostname = 'h.test'").Scan(&status, &lastErr, &failures)
	if status != "error" || lastErr != "i/o timeout" || failures != 1 {
		t.Fatalf("state after failure: %s %q %d", status, lastErr, failures)
	}
	// 5. Confirmed absence clears the current set without deleting history.
	if r := apply(5, dnsscan.Outcome{Type: "A", Status: dnsscan.StatusNoData}); r.Absent != 1 || rows() != 4 {
		t.Fatalf("step 5: %+v", r)
	}
	if c := current(t, pool); len(c) != 0 {
		t.Fatalf("current after removal: %v", c)
	}
	// 6. Records reappearing start a new version.
	if r := apply(6, found("192.0.2.1", "192.0.2.3")); r.NewVersions != 1 || rows() != 6 {
		t.Fatalf("step 6: %+v rows=%d", r, rows())
	}
	if n := count(t, pool, "SELECT count(DISTINCT updated_at) FROM dns_records WHERE hostname = 'h.test'"); n != 3 {
		t.Errorf("versions = %d, want 3", n)
	}
	var success time.Time
	_ = pool.QueryRow(ctx, "SELECT last_success_at FROM dns_scan_state WHERE hostname = 'h.test'").Scan(&success)
	if !success.Equal(t0.Add(6 * time.Hour).Truncate(time.Microsecond)) {
		t.Errorf("last_success_at = %v", success)
	}
}

func TestApplyStoresEveryTypeAndLongValues(t *testing.T) {
	s, pool := setup(t)
	long := make([]byte, 6000) // above the B-tree entry limit; index uses md5
	for i := range long {
		long[i] = 'a' + byte(i%26)
	}
	outcomes := []dnsscan.Outcome{
		found("192.0.2.1"),
		{Type: "TXT", Status: dnsscan.StatusFound, Records: []dnsscan.Record{{Type: "TXT", Value: string(long), TTL: 2147483647}}},
		{Type: "MX", Status: dnsscan.StatusNoData},
		{Type: "NS", Status: dnsscan.StatusError, Err: errors.New("SERVFAIL")},
	}
	res, err := s.ApplyDNSOutcomes(context.Background(), "h.test", outcomes, time.Now())
	if err != nil || res.NewVersions != 2 || res.Absent != 1 || res.Failed != 1 {
		t.Fatalf("res = %+v, %v", res, err)
	}
	var ttl int64
	_ = pool.QueryRow(context.Background(), "SELECT ttl FROM dns_records WHERE record_type = 'TXT'").Scan(&ttl)
	if ttl != 2147483647 {
		t.Errorf("max TTL stored as %d", ttl)
	}
	if n := count(t, pool, "SELECT count(*) FROM dns_scan_state WHERE hostname = 'h.test'"); n != 4 {
		t.Errorf("state rows = %d", n)
	}
}

func TestDuplicateValuesInAVersionAreRejected(t *testing.T) {
	s, _ := setup(t)
	dup := found("192.0.2.1", "192.0.2.1")
	if _, err := s.ApplyDNSOutcomes(context.Background(), "h.test", []dnsscan.Outcome{dup}, time.Now()); err == nil {
		t.Fatal("unique index must reject duplicate values within one version")
	}
}

func TestIncompleteDomains(t *testing.T) {
	s, pool := setup(t)
	exec(t, pool, `INSERT INTO domains (hostname, root_domain, asn, asn_name, waf) VALUES
		('a.test', 'a.test', 1, 'X', true), ('b.test', 'b.test', NULL, NULL, true),
		('c.test', 'c.test', 2, 'Y', NULL), ('d.test', 'd.test', NULL, NULL, NULL)`)
	got, err := s.IncompleteDomains(context.Background(), "", 10)
	if err != nil || !slices.Equal(got, []string{"b.test", "c.test", "d.test"}) {
		t.Fatalf("got %v, %v", got, err)
	}
	if got, _ := s.IncompleteDomains(context.Background(), "b.test", 1); !slices.Equal(got, []string{"c.test"}) {
		t.Errorf("keyset page: %v", got)
	}
}

func TestJobsAndLocks(t *testing.T) {
	s, _ := setup(t)
	ctx := context.Background()
	if last, err := s.LastCompleted(ctx, "daily"); err != nil || last != nil {
		t.Fatalf("never run: %v %v", last, err)
	}
	at := time.Date(2026, 10, 28, 0, 0, 5, 0, time.UTC)
	if err := s.JobStarted(ctx, "daily", at); err != nil {
		t.Fatal(err)
	}
	if last, _ := s.LastCompleted(ctx, "daily"); last != nil {
		t.Fatalf("started is not completed: %v", last)
	}
	if err := s.JobCompleted(ctx, "daily", at.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if last, _ := s.LastCompleted(ctx, "daily"); last == nil || !last.Equal(at.Add(time.Minute)) {
		t.Fatalf("completed: %v", last)
	}

	unlock, ok, err := s.TryLock(ctx, "active")
	if err != nil || !ok {
		t.Fatalf("first lock: %v %v", ok, err)
	}
	if _, ok, _ := s.TryLock(ctx, "active"); ok {
		t.Fatal("second session acquired a held lock")
	}
	unlockDaily, ok, _ := s.TryLock(ctx, "daily")
	if !ok {
		t.Fatal("different job lock must be independent")
	}
	unlockDaily() // a held lock pins a pooled connection until released
	unlock()
	if u, ok, _ := s.TryLock(ctx, "active"); !ok {
		t.Fatal("lock not released")
	} else {
		u()
	}
}
