// Package domainscan derives root domain, ASN and WAF data for hostnames.
package domainscan

import (
	"context"
	"log/slog"
	"net/netip"

	"github.com/miekg/dns"

	"github.com/reconvault/reconvault-dns-autoscan/internal/resolver"
)

// maxAddresses bounds the addresses checked per hostname.
const maxAddresses = 4

// Domain is the scan result stored in the domains table. Nil fields could
// not be determined reliably and are stored as NULL.
type Domain struct {
	Hostname   string
	RootDomain string
	ASN        *int64
	ASNName    *string
	WAF        *bool
}

// ASNResolver maps an address to its ASN.
type ASNResolver interface {
	Lookup(ctx context.Context, addr netip.Addr) (*ASN, error)
}

// WAFChecker detects WAFs.
type WAFChecker interface {
	Detect(ctx context.Context, host string, addrs []netip.Addr, cnames []string) WAFResult
}

// Scanner scans hostnames.
type Scanner struct {
	dns TXTLookuper
	asn ASNResolver
	waf WAFChecker
	log *slog.Logger
}

// NewScanner returns a Scanner.
func NewScanner(l TXTLookuper, asn ASNResolver, waf WAFChecker, log *slog.Logger) *Scanner {
	return &Scanner{dns: l, asn: asn, waf: waf, log: log}
}

// Scan derives the domain record for host. It never fails: fields that
// cannot be determined are left nil.
func (s *Scanner) Scan(ctx context.Context, host string) Domain {
	host = resolver.Canonical(host)
	d := Domain{Hostname: host, RootDomain: RootDomain(host)}

	addrs, cnames := s.addresses(ctx, host)
	d.ASN, d.ASNName = s.lookupASN(ctx, host, addrs)

	waf := s.waf.Detect(ctx, host, addrs, cnames)
	d.WAF = waf.Detected
	s.log.Debug("domain scanned", "hostname", host, "addresses", len(addrs), "waf", waf.Provider, "evidence", waf.Evidence)
	return d
}

// addresses returns up to maxAddresses public-or-private IPs of host (the
// literal itself for IP hosts) and the CNAME chain.
func (s *Scanner) addresses(ctx context.Context, host string) ([]netip.Addr, []string) {
	if addr, err := netip.ParseAddr(host); err == nil {
		return []netip.Addr{addr.Unmap()}, nil
	}
	var addrs []netip.Addr
	var chain []string
	for _, qt := range []uint16{dns.TypeA, dns.TypeAAAA} {
		res, err := s.dns.LookupRecursive(ctx, host, qt)
		if err != nil {
			continue
		}
		if len(chain) == 0 {
			chain = res.Chain
		}
		addrs = append(addrs, resolver.Addresses(res.Records)...)
	}
	if len(addrs) > maxAddresses {
		addrs = addrs[:maxAddresses]
	}
	return addrs, chain
}

// lookupASN returns the ASN shared by all of host's addresses. Differing
// ASNs, or none resolvable, yield nil rather than a guess.
func (s *Scanner) lookupASN(ctx context.Context, host string, addrs []netip.Addr) (*int64, *string) {
	var found *ASN
	for _, addr := range addrs {
		asn, err := s.asn.Lookup(ctx, addr)
		if err != nil {
			s.log.Debug("ASN lookup failed", "hostname", host, "address", addr, "error", err)
			continue
		}
		if asn == nil {
			continue
		}
		if found != nil && found.Number != asn.Number {
			s.log.Debug("addresses map to different ASNs", "hostname", host)
			return nil, nil
		}
		found = asn
	}
	if found == nil {
		return nil, nil
	}
	return &found.Number, &found.Name
}
