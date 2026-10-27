package domainscan

import (
	"context"
	"net/netip"
	"testing"
	"time"

	"github.com/reconvault/reconvault-dns-autoscan/internal/netguard"
	"github.com/reconvault/reconvault-dns-autoscan/internal/resolver"
	"github.com/reconvault/reconvault-dns-autoscan/internal/testdns"
)

func TestOriginName(t *testing.T) {
	if got := originName(netip.MustParseAddr("1.2.3.4")); got != "4.3.2.1.origin.asn.cymru.com" {
		t.Errorf("v4: %s", got)
	}
	want := "1.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.8.b.d.0.1.0.0.2.origin6.asn.cymru.com"
	if got := originName(netip.MustParseAddr("2001:db8::1")); got != want {
		t.Errorf("v6: %s", got)
	}
}

func TestParseOrigin(t *testing.T) {
	tests := []struct {
		name  string
		lines []string
		want  int64
		ok    bool
	}{
		{"single", []string{"13335 | 104.16.0.0/13 | US | arin | 2014-03-28"}, 13335, true},
		{"most specific prefix wins", []string{"100 | 10.0.0.0/8 | US | arin | x", "200 | 10.1.0.0/16 | US | arin | x"}, 200, true},
		{"MOAS is ambiguous", []string{"100 200 | 10.1.0.0/16 | US | arin | x"}, 0, false},
		{"MOAS on the most specific prefix", []string{"100 | 10.0.0.0/8 | US | arin | x", "200 300 | 10.1.0.0/16 | US | arin | x"}, 0, false},
		{"none", nil, 0, false},
		{"garbage", []string{"nonsense", "x | not-a-prefix | y"}, 0, false},
		{"invalid asn", []string{"AS13335 | 1.0.0.0/24 | US | arin | x"}, 0, false},
		{"out of range", []string{"4294967296 | 1.0.0.0/24 | US | arin | x"}, 0, false},
	}
	for _, tt := range tests {
		got, ok := parseOrigin(tt.lines)
		if got != tt.want || ok != tt.ok {
			t.Errorf("%s: got %d,%v want %d,%v", tt.name, got, ok, tt.want, tt.ok)
		}
	}
}

func TestParseASName(t *testing.T) {
	name, ok := parseASName(13335, []string{"13335 | US | arin | 2010-07-14 | CLOUDFLARENET, US"})
	if !ok || name != "CLOUDFLARENET" {
		t.Errorf("got %q %v", name, ok)
	}
	name, ok = parseASName(15169, []string{"15169 | US | arin | 2000-03-30 | GOOGLE - Google LLC, US"})
	if !ok || name != "GOOGLE - Google LLC" {
		t.Errorf("got %q %v", name, ok)
	}
	if _, ok := parseASName(1, []string{"2 | US | arin | x | OTHER, US"}); ok {
		t.Error("name for a different ASN must be rejected")
	}
	if _, ok := parseASName(1, []string{"1 | US | arin"}); ok {
		t.Error("short record must be rejected")
	}
}

func cymruServer(t *testing.T) *testdns.Server {
	return testdns.Start(t, []string{
		testdns.SOA("asn.cymru.com", 1),
		`10.2.0.192.origin.asn.cymru.com. 300 IN TXT "64500 | 192.0.2.0/24 | ZZ | test | 2020-01-01"`,
		`20.2.0.192.origin.asn.cymru.com. 300 IN TXT "64500 64501 | 192.0.2.0/24 | ZZ | test | 2020-01-01"`,
		`30.2.0.192.origin.asn.cymru.com. 300 IN TXT "64502 | 192.0.2.0/24 | ZZ | test | 2020-01-01"`,
		`AS64500.asn.cymru.com. 300 IN TXT "64500 | ZZ | test | 2020-01-01 | EXAMPLE-NET - Example Networks, ZZ"`,
	})
}

func TestCymruLookup(t *testing.T) {
	srv := cymruServer(t)
	c := NewCymruASN(resolver.New(resolver.Options{Recursive: []string{srv.Addr}, Timeout: time.Second, Policy: netguard.Policy{AllowPrivate: true}}))
	ctx := context.Background()

	asn, err := c.Lookup(ctx, netip.MustParseAddr("192.0.2.10"))
	if err != nil || asn == nil || asn.Number != 64500 || asn.Name != "EXAMPLE-NET - Example Networks" {
		t.Fatalf("lookup: %+v, %v", asn, err)
	}
	if asn, err := c.Lookup(ctx, netip.MustParseAddr("192.0.2.20")); err != nil || asn != nil {
		t.Errorf("MOAS: %+v, %v", asn, err)
	}
	if asn, err := c.Lookup(ctx, netip.MustParseAddr("192.0.2.30")); err != nil || asn != nil {
		t.Errorf("ASN without a name record must not be reported: %+v, %v", asn, err)
	}
	if asn, err := c.Lookup(ctx, netip.MustParseAddr("192.0.2.99")); err != nil || asn != nil {
		t.Errorf("unannounced: %+v, %v", asn, err)
	}
}
