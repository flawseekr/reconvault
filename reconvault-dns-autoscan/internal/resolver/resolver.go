// Package resolver performs DNS lookups that prefer the zone's
// authoritative nameservers, so record TTLs are the configured values
// rather than the counting-down remainders served from recursive caches.
// Recursive resolvers are used to locate zones and nameservers, for helper
// lookups (ASN), and as a fallback when authoritative servers fail.
package resolver

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"strings"
	"sync"
	"time"

	"github.com/miekg/dns"

	"github.com/reconvault/reconvault-dns-autoscan/internal/netguard"
)

// ErrLookupFailed marks lookups that produced no conclusive answer
// (timeouts, SERVFAIL, REFUSED, unreachable servers). It never means that
// records are absent.
var ErrLookupFailed = errors.New("dns lookup failed")

const (
	maxCNAMEChain = 8
	cacheTTL      = time.Hour
	maxCacheItems = 50_000
	maxNSServers  = 4
)

// Result is a conclusive answer for (Name, Type).
type Result struct {
	// Rcode is dns.RcodeSuccess or dns.RcodeNameError (NXDOMAIN).
	Rcode int
	// Records holds the RRs of the queried type for Name, following the
	// CNAME chain the way a resolving client would.
	Records []dns.RR
	// Chain lists the CNAME targets followed from Name, in order.
	Chain []string
	// Authoritative reports that the records came from authoritative
	// servers (exact TTLs) rather than a recursive cache.
	Authoritative bool
}

// Options configure a Resolver.
type Options struct {
	// Recursive lists recursive resolvers as host:port.
	Recursive []string
	Timeout   time.Duration
	Retries   int
	// AuthPort is the port used for authoritative servers ("53"; tests
	// override it).
	AuthPort string
	Policy   netguard.Policy
}

// Resolver is safe for concurrent use.
type Resolver struct {
	opts Options
	now  func() time.Time

	mu      sync.Mutex
	zones   map[string]cacheEntry // name -> zone apex
	servers map[string]cacheEntry // zone apex -> authoritative addresses
}

type cacheEntry struct {
	values  []string
	expires time.Time
}

// New returns a Resolver.
func New(opts Options) *Resolver {
	if opts.AuthPort == "" {
		opts.AuthPort = "53"
	}
	if opts.Timeout <= 0 {
		opts.Timeout = 5 * time.Second
	}
	return &Resolver{
		opts:    opts,
		now:     time.Now,
		zones:   make(map[string]cacheEntry),
		servers: make(map[string]cacheEntry),
	}
}

// Canonical lower-cases name and removes the trailing dot.
func Canonical(name string) string {
	return strings.TrimSuffix(strings.ToLower(strings.TrimSpace(name)), ".")
}

// Lookup resolves (name, qtype), preferring authoritative servers and
// falling back to the recursive resolvers. The error wraps ErrLookupFailed
// when no conclusive answer was obtained.
func (r *Resolver) Lookup(ctx context.Context, name string, qtype uint16) (*Result, error) {
	name = Canonical(name)
	res, authErr := r.lookupAuthoritative(ctx, name, qtype, 0)
	if authErr == nil {
		return res, nil
	}
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	res, err := r.LookupRecursive(ctx, name, qtype)
	if err != nil {
		return nil, fmt.Errorf("%w (authoritative: %v)", err, authErr)
	}
	return res, nil
}

// LookupRecursive resolves (name, qtype) through the recursive resolvers.
func (r *Resolver) LookupRecursive(ctx context.Context, name string, qtype uint16) (*Result, error) {
	name = Canonical(name)
	msg, err := r.exchangeAny(ctx, r.opts.Recursive, name, qtype, true, "udp")
	if err != nil {
		return nil, err
	}
	return extract(name, qtype, msg, false), nil
}

func (r *Resolver) lookupAuthoritative(ctx context.Context, name string, qtype uint16, depth int) (*Result, error) {
	servers, err := r.authServers(ctx, name)
	if err != nil {
		return nil, err
	}
	msg, err := r.exchangeAny(ctx, servers, name, qtype, false, "udp")
	if err == nil && !msg.Authoritative {
		// Some networks transparently intercept UDP port 53 and answer
		// from a cache without the AA flag. TCP is rarely intercepted, so
		// ask again over TCP before treating the server as lame.
		msg, err = r.exchangeAny(ctx, servers, name, qtype, false, "tcp")
	}
	if err != nil {
		return nil, err
	}
	if !msg.Authoritative {
		// A referral or a lame delegation: not a usable answer.
		return nil, fmt.Errorf("%w: non-authoritative answer for %s", ErrLookupFailed, name)
	}
	res := extract(name, qtype, msg, true)

	// The zone only holds its own data: follow a CNAME that leaves it.
	if qtype != dns.TypeCNAME && len(res.Records) == 0 && len(res.Chain) > 0 {
		if depth+len(res.Chain) >= maxCNAMEChain {
			return nil, fmt.Errorf("%w: CNAME chain too long for %s", ErrLookupFailed, name)
		}
		target := res.Chain[len(res.Chain)-1]
		sub, err := r.lookupAuthoritative(ctx, target, qtype, depth+len(res.Chain))
		if err != nil {
			return nil, err
		}
		res.Records = sub.Records
		res.Chain = append(res.Chain, sub.Chain...)
		res.Rcode = sub.Rcode
	}
	return res, nil
}

// extract collects the RRs of qtype for name from msg's answer section,
// following CNAMEs (bounded, loop-safe).
func extract(name string, qtype uint16, msg *dns.Msg, authoritative bool) *Result {
	res := &Result{Rcode: msg.Rcode, Authoritative: authoritative}
	owner := dns.Fqdn(name)
	seen := map[string]bool{owner: true}
	for hops := 0; hops <= maxCNAMEChain; hops++ {
		var next string
		for _, rr := range msg.Answer {
			h := rr.Header()
			if !strings.EqualFold(h.Name, owner) {
				continue
			}
			if h.Rrtype == qtype {
				res.Records = append(res.Records, rr)
			} else if cname, ok := rr.(*dns.CNAME); ok && qtype != dns.TypeCNAME && next == "" {
				next = dns.Fqdn(strings.ToLower(cname.Target))
			}
		}
		if len(res.Records) > 0 || next == "" || seen[next] {
			break
		}
		seen[next] = true
		res.Chain = append(res.Chain, Canonical(next))
		owner = next
	}
	return res
}

// authServers returns the authoritative server addresses for name's zone.
func (r *Resolver) authServers(ctx context.Context, name string) ([]string, error) {
	zone, err := r.findZone(ctx, name)
	if err != nil {
		return nil, err
	}
	if cached, ok := r.cached(r.servers, zone); ok {
		return cached, nil
	}

	nsRes, err := r.LookupRecursive(ctx, zone, dns.TypeNS)
	if err != nil {
		return nil, err
	}
	var addrs []string
	for _, rr := range nsRes.Records {
		ns, ok := rr.(*dns.NS)
		if !ok {
			continue
		}
		for _, qt := range []uint16{dns.TypeA, dns.TypeAAAA} {
			ipRes, err := r.LookupRecursive(ctx, ns.Ns, qt)
			if err != nil {
				continue
			}
			for _, ip := range addresses(ipRes.Records) {
				if r.opts.Policy.Allowed(ip) {
					addrs = append(addrs, net.JoinHostPort(ip.String(), r.opts.AuthPort))
				}
			}
		}
		if len(addrs) >= maxNSServers {
			break
		}
	}
	if len(addrs) == 0 {
		return nil, fmt.Errorf("%w: no usable nameservers for zone %s", ErrLookupFailed, zone)
	}
	r.store(r.servers, zone, addrs)
	return addrs, nil
}

// findZone returns the apex of the zone containing name, using the SOA
// record a recursive resolver returns in the answer (name is the apex) or
// the authority section (name is below the apex).
func (r *Resolver) findZone(ctx context.Context, name string) (string, error) {
	if cached, ok := r.cached(r.zones, name); ok {
		return cached[0], nil
	}
	msg, err := r.exchangeAny(ctx, r.opts.Recursive, name, dns.TypeSOA, true, "udp")
	if err != nil {
		return "", err
	}
	zone := ""
	for _, rr := range msg.Answer {
		if soa, ok := rr.(*dns.SOA); ok && strings.EqualFold(soa.Hdr.Name, dns.Fqdn(name)) {
			zone = name
		}
		if _, ok := rr.(*dns.CNAME); ok && strings.EqualFold(rr.Header().Name, dns.Fqdn(name)) && zone == "" {
			// An alias is never a zone apex; its zone is its parent's.
			parent := parentName(name)
			if parent == "" {
				return "", fmt.Errorf("%w: no zone for %s", ErrLookupFailed, name)
			}
			z, err := r.findZone(ctx, parent)
			if err != nil {
				return "", err
			}
			r.store(r.zones, name, []string{z})
			return z, nil
		}
	}
	if zone == "" {
		for _, rr := range msg.Ns {
			if soa, ok := rr.(*dns.SOA); ok {
				zone = Canonical(soa.Hdr.Name)
				break
			}
		}
	}
	if zone == "" {
		return "", fmt.Errorf("%w: no SOA found for %s", ErrLookupFailed, name)
	}
	r.store(r.zones, name, []string{zone})
	return zone, nil
}

func parentName(name string) string {
	_, parent, ok := strings.Cut(name, ".")
	if !ok {
		return ""
	}
	return parent
}

// exchangeAny sends (name, qtype) to servers in turn, with retries, and
// returns the first NOERROR or NXDOMAIN response. network is "udp" (with
// TCP fallback on truncation) or "tcp".
func (r *Resolver) exchangeAny(ctx context.Context, servers []string, name string, qtype uint16, recursion bool, network string) (*dns.Msg, error) {
	if len(servers) == 0 {
		return nil, fmt.Errorf("%w: no servers to query", ErrLookupFailed)
	}
	m := new(dns.Msg)
	m.SetQuestion(dns.Fqdn(name), qtype)
	m.RecursionDesired = recursion
	m.SetEdns0(dns.DefaultMsgSize, false)

	var lastErr error
	for attempt := 0; attempt <= r.opts.Retries; attempt++ {
		for _, server := range servers {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			resp, err := r.exchange(ctx, server, m, network)
			if err != nil {
				lastErr = err
				continue
			}
			switch resp.Rcode {
			case dns.RcodeSuccess, dns.RcodeNameError:
				return resp, nil
			default:
				lastErr = fmt.Errorf("%s from %s", dns.RcodeToString[resp.Rcode], server)
			}
		}
	}
	return nil, fmt.Errorf("%w: %s %s: %v", ErrLookupFailed, name, dns.TypeToString[qtype], lastErr)
}

// exchange sends m over network and retries a truncated UDP answer over TCP.
func (r *Resolver) exchange(ctx context.Context, server string, m *dns.Msg, network string) (*dns.Msg, error) {
	c := &dns.Client{Net: network, Timeout: r.opts.Timeout, UDPSize: dns.DefaultMsgSize}
	resp, _, err := c.ExchangeContext(ctx, m, server)
	if err == nil && resp.Truncated && network == "udp" {
		c.Net = "tcp"
		resp, _, err = c.ExchangeContext(ctx, m, server)
	}
	return resp, err
}

func (r *Resolver) cached(m map[string]cacheEntry, key string) ([]string, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	e, ok := m[key]
	if !ok || r.now().After(e.expires) {
		return nil, false
	}
	return e.values, true
}

func (r *Resolver) store(m map[string]cacheEntry, key string, values []string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(m) >= maxCacheItems {
		clear(m) // bounded memory; entries are cheap to rebuild
	}
	m[key] = cacheEntry{values: values, expires: r.now().Add(cacheTTL)}
}

// addresses returns the IPs of A and AAAA records.
func addresses(rrs []dns.RR) []netip.Addr {
	var out []netip.Addr
	for _, rr := range rrs {
		var ip net.IP
		switch v := rr.(type) {
		case *dns.A:
			ip = v.A
		case *dns.AAAA:
			ip = v.AAAA
		default:
			continue
		}
		if addr, ok := netip.AddrFromSlice(ip); ok {
			out = append(out, addr.Unmap())
		}
	}
	return out
}

// Addresses returns the IPs in A and AAAA records.
func Addresses(rrs []dns.RR) []netip.Addr { return addresses(rrs) }
