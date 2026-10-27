// Package testdns runs an in-process DNS server for tests. It serves the
// given zones authoritatively (queries without RD: AA set, own zone only,
// exact TTLs) and as a recursive resolver (queries with RD: CNAMEs followed
// across zones, TTLs reduced by CacheAge to mimic a cache). It listens on
// UDP and TCP on the same port.
package testdns

import (
	"net"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/miekg/dns"
)

// CacheAge is subtracted from TTLs in recursive answers.
const CacheAge = 100

// Server is a running test DNS server.
type Server struct {
	// Addr is host:port of both listeners.
	Addr string
	// Port is the listening port (use as resolver.Options.AuthPort).
	Port string

	mu        sync.Mutex
	zones     map[string][]dns.RR // apex -> records
	servfail  map[string]bool     // names answered with SERVFAIL
	lame      map[string]bool     // zones answered without AA
	truncate  map[string]bool     // names truncated over UDP
	intercept bool                // UDP answers lack AA, as from an interceptor
	queries   []string            // "name TYPE rd" log
}

// Start serves the zones, each given as zone-file lines (absolute names).
// The first record of each zone must be its SOA.
func Start(t testing.TB, zones ...[]string) *Server {
	t.Helper()
	s := &Server{
		zones:    map[string][]dns.RR{},
		servfail: map[string]bool{},
		lame:     map[string]bool{},
		truncate: map[string]bool{},
	}
	for _, lines := range zones {
		var rrs []dns.RR
		for _, l := range lines {
			rr, err := dns.NewRR(l)
			if err != nil {
				t.Fatalf("parse %q: %v", l, err)
			}
			rrs = append(rrs, rr)
		}
		soa, ok := rrs[0].(*dns.SOA)
		if !ok {
			t.Fatalf("zone must start with SOA: %q", lines[0])
		}
		s.zones[strings.ToLower(soa.Hdr.Name)] = rrs
	}

	var udp net.PacketConn
	var tcp net.Listener
	for i := 0; i < 20; i++ {
		var err error
		udp, err = net.ListenPacket("udp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		_, port, _ := net.SplitHostPort(udp.LocalAddr().String())
		if tcp, err = net.Listen("tcp", "127.0.0.1:"+port); err == nil {
			s.Port = port
			break
		}
		udp.Close()
	}
	if tcp == nil {
		t.Fatal("could not bind UDP and TCP on the same port")
	}
	s.Addr = "127.0.0.1:" + s.Port

	handler := dns.HandlerFunc(s.serve)
	us := &dns.Server{PacketConn: udp, Handler: handler}
	ts := &dns.Server{Listener: tcp, Handler: handler}
	go func() { _ = us.ActivateAndServe() }()
	go func() { _ = ts.ActivateAndServe() }()
	t.Cleanup(func() {
		_ = us.Shutdown()
		_ = ts.Shutdown()
	})
	return s
}

// Replace swaps the records of an existing zone (first line is the SOA).
func (s *Server) Replace(t testing.TB, lines []string) {
	t.Helper()
	var rrs []dns.RR
	for _, l := range lines {
		rr, err := dns.NewRR(l)
		if err != nil {
			t.Fatalf("parse %q: %v", l, err)
		}
		rrs = append(rrs, rr)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.zones[strings.ToLower(rrs[0].Header().Name)] = rrs
}

// Servfail makes the server answer SERVFAIL for name.
func (s *Server) Servfail(name string) { s.set(s.servfail, name) }

// Lame makes the server answer queries in zone without the AA flag.
func (s *Server) Lame(zone string) { s.set(s.lame, zone) }

// InterceptUDP makes authoritative answers over UDP lack the AA flag, like
// a network that transparently intercepts UDP port 53. TCP is unaffected.
func (s *Server) InterceptUDP() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.intercept = true
}

// Truncate makes UDP answers for name truncated (forcing TCP).
func (s *Server) Truncate(name string) { s.set(s.truncate, name) }

func (s *Server) set(m map[string]bool, name string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	m[dns.Fqdn(strings.ToLower(name))] = true
}

// Queries returns the logged queries ("name TYPE rd|nord").
func (s *Server) Queries() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.queries...)
}

func (s *Server) serve(w dns.ResponseWriter, req *dns.Msg) {
	q := req.Question[0]
	name := strings.ToLower(q.Name)
	mode := "nord"
	if req.RecursionDesired {
		mode = "rd"
	}

	s.mu.Lock()
	s.queries = append(s.queries, strings.TrimSuffix(name, ".")+" "+dns.TypeToString[q.Qtype]+" "+mode)
	servfail, truncate := s.servfail[name], s.truncate[name]
	intercept := s.intercept && w.LocalAddr().Network() == "udp"
	s.mu.Unlock()

	resp := new(dns.Msg)
	resp.SetReply(req)
	switch {
	case servfail:
		resp.Rcode = dns.RcodeServerFailure
	case truncate && w.LocalAddr().Network() == "udp":
		resp.Truncated = true
	case req.RecursionDesired:
		resp.RecursionAvailable = true
		s.recursive(resp, name, q.Qtype, 0)
		for _, rr := range append(resp.Answer, resp.Ns...) {
			if rr.Header().Ttl > CacheAge {
				rr.Header().Ttl -= CacheAge
			}
		}
	default:
		s.authoritative(resp, name, q.Qtype)
		if intercept {
			resp.Authoritative = false
		}
	}
	_ = w.WriteMsg(resp)
}

// zoneFor returns the apex and records of the zone containing name.
func (s *Server) zoneFor(name string) (string, []dns.RR) {
	s.mu.Lock()
	defer s.mu.Unlock()
	best := ""
	for apex := range s.zones {
		if (name == apex || strings.HasSuffix(name, "."+apex)) && len(apex) > len(best) {
			best = apex
		}
	}
	if best == "" {
		return "", nil
	}
	return best, s.zones[best]
}

// answer returns name's records of qtype, or its CNAME, from the zone.
func answer(rrs []dns.RR, name string, qtype uint16) (records []dns.RR, cname *dns.CNAME, exists bool) {
	for _, rr := range rrs {
		owner := strings.ToLower(rr.Header().Name)
		if owner == name || strings.HasSuffix(owner, "."+name) {
			exists = true // name or an empty non-terminal above a record
		}
		if owner != name {
			continue
		}
		if rr.Header().Rrtype == qtype {
			records = append(records, dns.Copy(rr))
		} else if c, ok := rr.(*dns.CNAME); ok {
			cname = dns.Copy(c).(*dns.CNAME)
		}
	}
	return records, cname, exists
}

func soaOf(rrs []dns.RR) dns.RR { return dns.Copy(rrs[0]) }

func (s *Server) authoritative(resp *dns.Msg, name string, qtype uint16) {
	apex, rrs := s.zoneFor(name)
	if rrs == nil {
		resp.Rcode = dns.RcodeRefused
		return
	}
	s.mu.Lock()
	lame := s.lame[apex]
	s.mu.Unlock()
	resp.Authoritative = !lame
	records, cname, exists := answer(rrs, name, qtype)
	switch {
	case len(records) > 0:
		resp.Answer = records
	case cname != nil:
		resp.Answer = []dns.RR{cname} // targets outside the zone are not followed
	case exists:
		resp.Ns = []dns.RR{soaOf(rrs)}
	default:
		resp.Rcode = dns.RcodeNameError
		resp.Ns = []dns.RR{soaOf(rrs)}
	}
}

func (s *Server) recursive(resp *dns.Msg, name string, qtype uint16, depth int) {
	_, rrs := s.zoneFor(name)
	if rrs == nil || depth > 8 {
		resp.Rcode = dns.RcodeNameError
		return
	}
	records, cname, exists := answer(rrs, name, qtype)
	switch {
	case len(records) > 0:
		resp.Answer = append(resp.Answer, records...)
	case cname != nil:
		resp.Answer = append(resp.Answer, cname)
		s.recursive(resp, strings.ToLower(cname.Target), qtype, depth+1)
	case exists:
		resp.Ns = []dns.RR{soaOf(rrs)}
	default:
		resp.Rcode = dns.RcodeNameError
		resp.Ns = []dns.RR{soaOf(rrs)}
	}
}

// SOA returns a SOA line for zone (helper for building zones).
func SOA(zone string, serial int) string {
	return zone + ". 3600 IN SOA ns1." + zone + ". hostmaster." + zone + ". " + strconv.Itoa(serial) + " 7200 900 1209600 300"
}
