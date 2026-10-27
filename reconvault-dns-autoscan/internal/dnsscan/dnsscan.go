// Package dnsscan discovers and normalizes the DNS records of a hostname.
package dnsscan

import (
	"context"
	"fmt"
	"net"
	"net/netip"
	"sort"
	"strings"

	"github.com/miekg/dns"

	"github.com/reconvault/reconvault-dns-autoscan/internal/resolver"
)

// RecordTypes are queried for every hostname.
var RecordTypes = []uint16{
	dns.TypeA, dns.TypeAAAA, dns.TypeCNAME, dns.TypeMX, dns.TypeNS,
	dns.TypeTXT, dns.TypeSOA, dns.TypeCAA, dns.TypeHTTPS,
}

// Status classifies one lookup.
type Status string

const (
	// StatusFound: records were returned.
	StatusFound Status = "found"
	// StatusNoData: the name exists but has no records of this type.
	StatusNoData Status = "nodata"
	// StatusNXDomain: the name does not exist.
	StatusNXDomain Status = "nxdomain"
	// StatusError: no conclusive answer (timeout, SERVFAIL, ...). Says
	// nothing about whether records exist.
	StatusError Status = "error"
)

// Record is one normalized record.
type Record struct {
	Type  string
	Value string
	TTL   uint32
}

// Outcome is the result of looking up one record type.
type Outcome struct {
	Type    string
	Status  Status
	Records []Record // sorted by value, unique values
	Err     error
}

// Lookuper resolves DNS queries.
type Lookuper interface {
	Lookup(ctx context.Context, name string, qtype uint16) (*resolver.Result, error)
}

// Scanner scans hostnames.
type Scanner struct {
	dns Lookuper
}

// New returns a Scanner.
func New(l Lookuper) *Scanner { return &Scanner{dns: l} }

// Scan looks up every record type for host. IP literals get a PTR lookup
// of their reverse name instead.
func (s *Scanner) Scan(ctx context.Context, host string) []Outcome {
	host = resolver.Canonical(host)
	if addr, err := netip.ParseAddr(host); err == nil {
		reverse, err := dns.ReverseAddr(addr.String())
		if err != nil {
			return []Outcome{{Type: "PTR", Status: StatusError, Err: err}}
		}
		return []Outcome{s.lookup(ctx, reverse, dns.TypePTR)}
	}
	out := make([]Outcome, 0, len(RecordTypes))
	for _, qt := range RecordTypes {
		if ctx.Err() != nil {
			break
		}
		out = append(out, s.lookup(ctx, host, qt))
	}
	return out
}

func (s *Scanner) lookup(ctx context.Context, name string, qtype uint16) Outcome {
	typ := dns.TypeToString[qtype]
	res, err := s.dns.Lookup(ctx, name, qtype)
	if err != nil {
		return Outcome{Type: typ, Status: StatusError, Err: err}
	}
	records := Normalize(res.Records, qtype)
	switch {
	case len(records) > 0:
		return Outcome{Type: typ, Status: StatusFound, Records: records}
	case res.Rcode == dns.RcodeNameError && len(res.Chain) == 0:
		return Outcome{Type: typ, Status: StatusNXDomain}
	default:
		// NOERROR without data, or an alias whose target lacks this type.
		return Outcome{Type: typ, Status: StatusNoData}
	}
}

// Normalize converts RRs of qtype into unique records sorted by value. For
// duplicate values the lowest TTL is kept.
func Normalize(rrs []dns.RR, qtype uint16) []Record {
	typ := dns.TypeToString[qtype]
	byValue := make(map[string]uint32)
	for _, rr := range rrs {
		if rr.Header().Rrtype != qtype {
			continue
		}
		value, ok := Value(rr)
		if !ok {
			continue
		}
		ttl := rr.Header().Ttl
		if prev, dup := byValue[value]; !dup || ttl < prev {
			byValue[value] = ttl
		}
	}
	out := make([]Record, 0, len(byValue))
	for v, ttl := range byValue {
		out = append(out, Record{Type: typ, Value: v, TTL: ttl})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Value < out[j].Value })
	return out
}

// name normalizes a domain name in RDATA. The root name stays "." so that
// null MX (RFC 7505, "0 .") and SRV "no service" targets keep their meaning.
func name(n string) string {
	if c := resolver.Canonical(n); c != "" {
		return c
	}
	return "."
}

// Value renders the RDATA of rr in a normalized presentation form that
// keeps every record-specific field (priorities, weights, ports, SOA
// timers, CAA flags and tags). Names are lower-cased without the trailing
// dot; TXT strings are concatenated in their escaped presentation form.
func Value(rr dns.RR) (string, bool) {
	var v string
	switch r := rr.(type) {
	case *dns.A:
		ip, ok := netip.AddrFromSlice(r.A.To4())
		if !ok {
			return "", false
		}
		v = ip.String()
	case *dns.AAAA:
		ip, ok := netip.AddrFromSlice(r.AAAA)
		if !ok || len(r.AAAA) != net.IPv6len {
			return "", false
		}
		v = ip.String()
	case *dns.CNAME:
		v = name(r.Target)
	case *dns.NS:
		v = name(r.Ns)
	case *dns.PTR:
		v = name(r.Ptr)
	case *dns.MX:
		v = fmt.Sprintf("%d %s", r.Preference, name(r.Mx))
	case *dns.TXT:
		v = strings.Join(r.Txt, "")
	case *dns.SOA:
		v = fmt.Sprintf("%s %s %d %d %d %d %d", name(r.Ns), name(r.Mbox), r.Serial, r.Refresh, r.Retry, r.Expire, r.Minttl)
	case *dns.CAA:
		v = fmt.Sprintf("%d %s %q", r.Flag, strings.ToLower(r.Tag), r.Value)
	case *dns.SRV:
		v = fmt.Sprintf("%d %d %d %s", r.Priority, r.Weight, r.Port, name(r.Target))
	default:
		// HTTPS, SVCB and others: miekg's presentation form of the RDATA.
		v = strings.TrimSpace(strings.TrimPrefix(rr.String(), rr.Header().String()))
	}
	return v, v != ""
}
