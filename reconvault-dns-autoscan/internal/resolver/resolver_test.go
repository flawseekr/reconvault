package resolver_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/miekg/dns"

	"github.com/reconvault/reconvault-dns-autoscan/internal/netguard"
	"github.com/reconvault/reconvault-dns-autoscan/internal/resolver"
	"github.com/reconvault/reconvault-dns-autoscan/internal/testdns"
)

func zones() [][]string {
	return [][]string{
		{
			testdns.SOA("example.test", 1),
			"example.test. 3600 IN NS ns1.example.test.",
			"ns1.example.test. 3600 IN A 127.0.0.1",
			"example.test. 300 IN A 192.0.2.10",
			"www.example.test. 600 IN CNAME edge.cdn.test.",
			"big.example.test. 300 IN TXT \"" + strings.Repeat("x", 250) + "\" \"" + strings.Repeat("y", 250) + "\"",
			"sub.dept.example.test. 300 IN A 192.0.2.20",
		},
		{
			testdns.SOA("cdn.test", 1),
			"cdn.test. 3600 IN NS ns1.example.test.",
			"edge.cdn.test. 120 IN A 198.51.100.7",
			"edge.cdn.test. 120 IN A 198.51.100.8",
		},
		{
			testdns.SOA("lame.test", 1),
			"lame.test. 3600 IN NS ns1.example.test.",
			"lame.test. 500 IN A 192.0.2.30",
		},
	}
}

func newResolver(srv *testdns.Server, allowPrivate bool) *resolver.Resolver {
	return resolver.New(resolver.Options{
		Recursive: []string{srv.Addr},
		Timeout:   time.Second,
		Retries:   1,
		AuthPort:  srv.Port,
		Policy:    netguard.Policy{AllowPrivate: allowPrivate},
	})
}

func ttls(rrs []dns.RR) []uint32 {
	var out []uint32
	for _, rr := range rrs {
		out = append(out, rr.Header().Ttl)
	}
	return out
}

func TestLookupUsesAuthoritativeTTL(t *testing.T) {
	srv := testdns.Start(t, zones()...)
	res, err := newResolver(srv, true).Lookup(context.Background(), "Example.TEST.", dns.TypeA)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Authoritative || len(res.Records) != 1 || res.Records[0].(*dns.A).A.String() != "192.0.2.10" {
		t.Fatalf("result = %+v", res)
	}
	if got := res.Records[0].Header().Ttl; got != 300 {
		t.Errorf("TTL = %d, want the authoritative 300 (recursive cache would give %d)", got, 300-testdns.CacheAge)
	}
	var authQuery bool
	for _, q := range srv.Queries() {
		if q == "example.test A nord" {
			authQuery = true
		}
	}
	if !authQuery {
		t.Errorf("no non-recursive query sent: %v", srv.Queries())
	}
}

func TestLookupFollowsCNAMEAcrossZones(t *testing.T) {
	srv := testdns.Start(t, zones()...)
	res, err := newResolver(srv, true).Lookup(context.Background(), "www.example.test", dns.TypeA)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Authoritative || len(res.Records) != 2 || strings.Join(res.Chain, ",") != "edge.cdn.test" {
		t.Fatalf("result = %+v", res)
	}
	for _, ttl := range ttls(res.Records) {
		if ttl != 120 {
			t.Errorf("TTL = %d, want 120", ttl)
		}
	}
	cname, err := newResolver(srv, true).Lookup(context.Background(), "www.example.test", dns.TypeCNAME)
	if err != nil || len(cname.Records) != 1 || cname.Records[0].(*dns.CNAME).Target != "edge.cdn.test." || cname.Records[0].Header().Ttl != 600 {
		t.Fatalf("CNAME = %+v, %v", cname, err)
	}
}

func TestLookupNXDomainAndNoData(t *testing.T) {
	srv := testdns.Start(t, zones()...)
	r := newResolver(srv, true)
	nx, err := r.Lookup(context.Background(), "missing.example.test", dns.TypeA)
	if err != nil || nx.Rcode != dns.RcodeNameError || len(nx.Records) != 0 {
		t.Fatalf("NXDOMAIN: %+v, %v", nx, err)
	}
	nodata, err := r.Lookup(context.Background(), "example.test", dns.TypeMX)
	if err != nil || nodata.Rcode != dns.RcodeSuccess || len(nodata.Records) != 0 {
		t.Fatalf("NODATA: %+v, %v", nodata, err)
	}
	// Empty non-terminal: exists without records.
	ent, err := r.Lookup(context.Background(), "dept.example.test", dns.TypeA)
	if err != nil || ent.Rcode != dns.RcodeSuccess {
		t.Fatalf("empty non-terminal: %+v, %v", ent, err)
	}
}

func TestLookupServfailIsAFailureNotAbsence(t *testing.T) {
	srv := testdns.Start(t, zones()...)
	srv.Servfail("broken.example.test")
	_, err := newResolver(srv, true).Lookup(context.Background(), "broken.example.test", dns.TypeA)
	if !errors.Is(err, resolver.ErrLookupFailed) {
		t.Fatalf("err = %v, want ErrLookupFailed", err)
	}
}

func TestLookupFallsBackToRecursiveForLameZones(t *testing.T) {
	srv := testdns.Start(t, zones()...)
	srv.Lame("lame.test")
	res, err := newResolver(srv, true).Lookup(context.Background(), "lame.test", dns.TypeA)
	if err != nil {
		t.Fatal(err)
	}
	if res.Authoritative || len(res.Records) != 1 || res.Records[0].Header().Ttl != 500-testdns.CacheAge {
		t.Fatalf("fallback result = %+v", res)
	}
}

func TestLookupNeverQueriesBlockedNameservers(t *testing.T) {
	srv := testdns.Start(t, zones()...)
	// ns1.example.test is 127.0.0.1: with the policy enforced it must not
	// be queried, and the configured recursive resolver answers instead.
	res, err := newResolver(srv, false).Lookup(context.Background(), "example.test", dns.TypeA)
	if err != nil {
		t.Fatal(err)
	}
	if res.Authoritative {
		t.Error("blocked nameserver was used")
	}
	for _, q := range srv.Queries() {
		if strings.HasSuffix(q, " nord") {
			t.Errorf("non-recursive query sent to blocked nameserver: %s", q)
		}
	}
}

func TestLookupRetriesTruncatedAnswersOverTCP(t *testing.T) {
	srv := testdns.Start(t, zones()...)
	srv.Truncate("big.example.test")
	res, err := newResolver(srv, true).Lookup(context.Background(), "big.example.test", dns.TypeTXT)
	if err != nil || len(res.Records) != 1 {
		t.Fatalf("TXT over TCP: %+v, %v", res, err)
	}
	if got := strings.Join(res.Records[0].(*dns.TXT).Txt, ""); len(got) != 500 {
		t.Errorf("TXT length = %d", len(got))
	}
}

func TestZoneAndNameserverLookupsAreCached(t *testing.T) {
	srv := testdns.Start(t, zones()...)
	r := newResolver(srv, true)
	for _, qt := range []uint16{dns.TypeA, dns.TypeTXT, dns.TypeMX, dns.TypeNS} {
		if _, err := r.Lookup(context.Background(), "example.test", qt); err != nil {
			t.Fatal(err)
		}
	}
	count := func(q string) int {
		n := 0
		for _, x := range srv.Queries() {
			if x == q {
				n++
			}
		}
		return n
	}
	if n := count("example.test SOA rd"); n != 1 {
		t.Errorf("zone discovery queries = %d, want 1", n)
	}
	if n := count("ns1.example.test A rd"); n != 1 {
		t.Errorf("nameserver address queries = %d, want 1", n)
	}
}

func TestLookupRespectsCancellation(t *testing.T) {
	srv := testdns.Start(t, zones()...)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := newResolver(srv, true).Lookup(ctx, "example.test", dns.TypeA); err == nil {
		t.Fatal("expected error for cancelled context")
	}
}

func TestLookupRetriesOverTCPWhenUDPIsIntercepted(t *testing.T) {
	srv := testdns.Start(t, zones()...)
	srv.InterceptUDP()
	res, err := newResolver(srv, true).Lookup(context.Background(), "example.test", dns.TypeA)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Authoritative || res.Records[0].Header().Ttl != 300 {
		t.Fatalf("intercepted UDP was not bypassed over TCP: %+v", res)
	}
}
