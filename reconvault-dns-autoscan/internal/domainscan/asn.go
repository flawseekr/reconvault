package domainscan

import (
	"context"
	"fmt"
	"net/netip"
	"regexp"
	"strconv"
	"strings"

	"github.com/miekg/dns"

	"github.com/reconvault/reconvault-dns-autoscan/internal/resolver"
)

// ASN identifies an Autonomous System.
type ASN struct {
	Number int64
	Name   string
}

// TXTLookuper performs recursive DNS lookups.
type TXTLookuper interface {
	LookupRecursive(ctx context.Context, name string, qtype uint16) (*resolver.Result, error)
}

// CymruASN resolves IP addresses to ASNs with Team Cymru's IP-to-ASN DNS
// service (https://www.team-cymru.com/ip-asn-mapping): origin lookups map
// an address to the ASN announcing its most specific prefix, and AS
// lookups map that ASN to its registered name. Both answers come from the
// same dataset, so number and name always describe the same ASN.
type CymruASN struct {
	dns TXTLookuper
}

// NewCymruASN returns an ASN resolver backed by Team Cymru.
func NewCymruASN(l TXTLookuper) *CymruASN { return &CymruASN{dns: l} }

// Lookup returns the ASN announcing addr, or nil when it cannot be
// determined reliably: unannounced or non-public space, several origin
// ASNs for the prefix (MOAS), or a failed lookup (err != nil).
func (c *CymruASN) Lookup(ctx context.Context, addr netip.Addr) (*ASN, error) {
	addr = addr.Unmap()
	res, err := c.dns.LookupRecursive(ctx, originName(addr), dns.TypeTXT)
	if err != nil {
		return nil, err
	}
	number, ok := parseOrigin(txtStrings(res.Records))
	if !ok {
		return nil, nil
	}
	nameRes, err := c.dns.LookupRecursive(ctx, fmt.Sprintf("AS%d.asn.cymru.com", number), dns.TypeTXT)
	if err != nil {
		return nil, err
	}
	name, ok := parseASName(number, txtStrings(nameRes.Records))
	if !ok {
		return nil, nil // never report a number without its matching name
	}
	return &ASN{Number: number, Name: name}, nil
}

func originName(addr netip.Addr) string {
	if addr.Is4() {
		b := addr.As4()
		return fmt.Sprintf("%d.%d.%d.%d.origin.asn.cymru.com", b[3], b[2], b[1], b[0])
	}
	b := addr.As16()
	var sb strings.Builder
	for i := len(b) - 1; i >= 0; i-- {
		fmt.Fprintf(&sb, "%x.%x.", b[i]&0x0f, b[i]>>4)
	}
	sb.WriteString("origin6.asn.cymru.com")
	return sb.String()
}

func txtStrings(rrs []dns.RR) []string {
	var out []string
	for _, rr := range rrs {
		if t, ok := rr.(*dns.TXT); ok {
			out = append(out, strings.Join(t.Txt, ""))
		}
	}
	return out
}

// parseOrigin picks the record with the most specific prefix from
// "ASN[ ASN...] | prefix | CC | registry | date" lines and returns its ASN
// when exactly one is listed.
func parseOrigin(lines []string) (int64, bool) {
	bestBits := -1
	var bestASNs []string
	for _, line := range lines {
		f := strings.Split(line, "|")
		if len(f) < 2 {
			continue
		}
		prefix, err := netip.ParsePrefix(strings.TrimSpace(f[1]))
		if err != nil {
			continue
		}
		if prefix.Bits() > bestBits {
			bestBits = prefix.Bits()
			bestASNs = strings.Fields(f[0])
		}
	}
	if len(bestASNs) != 1 {
		return 0, false // none, or multiple origin ASNs (ambiguous)
	}
	n, err := strconv.ParseInt(bestASNs[0], 10, 64)
	if err != nil || n <= 0 || n > 4294967295 {
		return 0, false
	}
	return n, true
}

var countrySuffix = regexp.MustCompile(`,\s*[A-Z]{2}$`)

// parseASName extracts the name from "ASN | CC | registry | date | NAME, CC"
// and verifies the record describes the requested ASN.
func parseASName(number int64, lines []string) (string, bool) {
	for _, line := range lines {
		f := strings.Split(line, "|")
		if len(f) < 5 {
			continue
		}
		if n, err := strconv.ParseInt(strings.TrimSpace(f[0]), 10, 64); err != nil || n != number {
			continue
		}
		name := strings.TrimSpace(countrySuffix.ReplaceAllString(strings.TrimSpace(f[4]), ""))
		if name != "" {
			return name, true
		}
	}
	return "", false
}
