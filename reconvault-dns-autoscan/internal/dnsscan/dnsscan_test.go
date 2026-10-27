package dnsscan_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/miekg/dns"

	"github.com/reconvault/reconvault-dns-autoscan/internal/dnsscan"
	"github.com/reconvault/reconvault-dns-autoscan/internal/netguard"
	"github.com/reconvault/reconvault-dns-autoscan/internal/resolver"
	"github.com/reconvault/reconvault-dns-autoscan/internal/testdns"
)

func rr(t *testing.T, s string) dns.RR {
	t.Helper()
	r, err := dns.NewRR(s)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestValue(t *testing.T) {
	tests := []struct{ rr, want string }{
		{"a.test. 60 IN A 192.0.2.1", "192.0.2.1"},
		{"a.test. 60 IN AAAA 2001:DB8:0:0:0:0:0:1", "2001:db8::1"},
		{"a.test. 60 IN CNAME Target.Example.TEST.", "target.example.test"},
		{"a.test. 60 IN NS NS1.Example.test.", "ns1.example.test"},
		{"a.test. 60 IN MX 10 Mail.Example.test.", "10 mail.example.test"},
		{"a.test. 60 IN MX 0 .", "0 ."}, // null MX (RFC 7505)
		{"_x._tcp.a.test. 60 IN SRV 0 0 0 .", "0 0 0 ."},
		{`a.test. 60 IN TXT "v=spf1 " "include:_spf.example.test -all"`, "v=spf1 include:_spf.example.test -all"},
		{`a.test. 60 IN TXT "say \"hi\""`, `say \"hi\"`},
		{"a.test. 60 IN SOA ns1.a.test. hostmaster.a.test. 2026102801 7200 900 1209600 300", "ns1.a.test hostmaster.a.test 2026102801 7200 900 1209600 300"},
		{`a.test. 60 IN CAA 0 issue "letsencrypt.org"`, `0 issue "letsencrypt.org"`},
		{`a.test. 60 IN CAA 128 IODEF "mailto:sec@a.test"`, `128 iodef "mailto:sec@a.test"`},
		{"_sip._tcp.a.test. 60 IN SRV 10 20 5060 SIP.a.test.", "10 20 5060 sip.a.test"},
		{"1.2.0.192.in-addr.arpa. 60 IN PTR Host.A.test.", "host.a.test"},
		{`a.test. 60 IN HTTPS 1 . alpn="h3,h2"`, `1 . alpn="h3,h2"`},
	}
	for _, tt := range tests {
		got, ok := dnsscan.Value(rr(t, tt.rr))
		if !ok || got != tt.want {
			t.Errorf("%s: got %q (ok=%v), want %q", tt.rr, got, ok, tt.want)
		}
	}
}

func TestNormalizeDedupesSortsAndKeepsLowestTTL(t *testing.T) {
	rrs := []dns.RR{
		rr(t, "a.test. 300 IN A 192.0.2.2"),
		rr(t, "a.test. 300 IN A 192.0.2.1"),
		rr(t, "a.test. 100 IN A 192.0.2.1"),
		rr(t, "a.test. 300 IN AAAA 2001:db8::1"), // other type: ignored
	}
	got := dnsscan.Normalize(rrs, dns.TypeA)
	want := []dnsscan.Record{{Type: "A", Value: "192.0.2.1", TTL: 100}, {Type: "A", Value: "192.0.2.2", TTL: 300}}
	if len(got) != 2 || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("got %+v", got)
	}
}

func scanner(t *testing.T) (*dnsscan.Scanner, *testdns.Server) {
	srv := testdns.Start(t,
		[]string{
			testdns.SOA("example.test", 7),
			"example.test. 3600 IN NS ns1.example.test.",
			"ns1.example.test. 3600 IN A 127.0.0.1",
			"example.test. 300 IN A 192.0.2.10",
			"example.test. 300 IN A 192.0.2.11",
			"example.test. 300 IN AAAA 2001:db8::10",
			"example.test. 3600 IN MX 10 mail.example.test.",
			"example.test. 3600 IN MX 20 backup.example.test.",
			`example.test. 300 IN TXT "v=spf1 -all"`,
			`example.test. 300 IN TXT "google-site-verification=abc"`,
			`example.test. 3600 IN CAA 0 issue "letsencrypt.org"`,
			"www.example.test. 600 IN CNAME example.test.",
		},
		[]string{
			testdns.SOA("2.0.192.in-addr.arpa", 1),
			"2.0.192.in-addr.arpa. 3600 IN NS ns1.example.test.",
			"10.2.0.192.in-addr.arpa. 900 IN PTR example.test.",
		},
	)
	r := resolver.New(resolver.Options{Recursive: []string{srv.Addr}, Timeout: time.Second, AuthPort: srv.Port, Policy: netguard.Policy{AllowPrivate: true}})
	return dnsscan.New(r), srv
}

func byType(outcomes []dnsscan.Outcome) map[string]dnsscan.Outcome {
	m := map[string]dnsscan.Outcome{}
	for _, o := range outcomes {
		m[o.Type] = o
	}
	return m
}

func values(o dnsscan.Outcome) []string {
	var out []string
	for _, r := range o.Records {
		out = append(out, r.Value)
	}
	return out
}

func TestScanApex(t *testing.T) {
	s, _ := scanner(t)
	got := byType(s.Scan(context.Background(), "Example.Test."))
	if len(got) != len(dnsscan.RecordTypes) {
		t.Fatalf("outcomes = %d", len(got))
	}
	expect := map[string][]string{
		"A":    {"192.0.2.10", "192.0.2.11"},
		"AAAA": {"2001:db8::10"},
		"MX":   {"10 mail.example.test", "20 backup.example.test"},
		"TXT":  {"google-site-verification=abc", "v=spf1 -all"},
		"CAA":  {`0 issue "letsencrypt.org"`},
		"NS":   {"ns1.example.test"},
		"SOA":  {"ns1.example.test hostmaster.example.test 7 7200 900 1209600 300"},
	}
	for typ, want := range expect {
		o := got[typ]
		if o.Status != dnsscan.StatusFound || len(values(o)) != len(want) {
			t.Errorf("%s: %+v", typ, o)
			continue
		}
		for i := range want {
			if values(o)[i] != want[i] {
				t.Errorf("%s[%d] = %q, want %q", typ, i, values(o)[i], want[i])
			}
		}
	}
	if got["A"].Records[0].TTL != 300 || got["MX"].Records[0].TTL != 3600 {
		t.Errorf("TTLs not authoritative: A %d, MX %d", got["A"].Records[0].TTL, got["MX"].Records[0].TTL)
	}
	for _, typ := range []string{"CNAME", "HTTPS"} {
		if got[typ].Status != dnsscan.StatusNoData {
			t.Errorf("%s: status %s, want nodata", typ, got[typ].Status)
		}
	}
}

func TestScanAliasAndMissingNames(t *testing.T) {
	s, _ := scanner(t)
	alias := byType(s.Scan(context.Background(), "www.example.test"))
	if alias["CNAME"].Status != dnsscan.StatusFound || values(alias["CNAME"])[0] != "example.test" {
		t.Errorf("CNAME: %+v", alias["CNAME"])
	}
	if alias["A"].Status != dnsscan.StatusFound || len(alias["A"].Records) != 2 {
		t.Errorf("A through alias: %+v", alias["A"])
	}
	missing := byType(s.Scan(context.Background(), "nope.example.test"))
	for typ, o := range missing {
		if o.Status != dnsscan.StatusNXDomain || len(o.Records) != 0 {
			t.Errorf("%s: %+v", typ, o)
		}
	}
}

func TestScanIPLiteralUsesPTR(t *testing.T) {
	s, _ := scanner(t)
	got := s.Scan(context.Background(), "192.0.2.10")
	if len(got) != 1 || got[0].Type != "PTR" || got[0].Status != dnsscan.StatusFound || got[0].Records[0].Value != "example.test" || got[0].Records[0].TTL != 900 {
		t.Fatalf("PTR: %+v", got)
	}
}

func TestScanReportsFailuresAsErrors(t *testing.T) {
	s, srv := scanner(t)
	srv.Servfail("broken.example.test")
	for _, o := range s.Scan(context.Background(), "broken.example.test") {
		if o.Status != dnsscan.StatusError || !errors.Is(o.Err, resolver.ErrLookupFailed) {
			t.Errorf("%s: %+v", o.Type, o)
		}
	}
}
