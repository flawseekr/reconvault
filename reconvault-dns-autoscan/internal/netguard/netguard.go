// Package netguard restricts outbound connections to public addresses so
// hostnames taken from the database cannot steer scans at internal
// networks (SSRF). It mirrors the policy of the ReconVault API.
package netguard

import (
	"errors"
	"net/netip"
	"syscall"
)

// ErrBlocked is returned for connections to non-public addresses.
var ErrBlocked = errors.New("destination address is not allowed")

// nonPublicPrefixes lists special-purpose ranges (RFC 6890 and successors)
// that are not covered by the netip.Addr predicates used in IsPublic.
var nonPublicPrefixes = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"),
	netip.MustParsePrefix("100.64.0.0/10"),
	netip.MustParsePrefix("192.0.0.0/24"),
	netip.MustParsePrefix("192.0.2.0/24"),
	netip.MustParsePrefix("198.18.0.0/15"),
	netip.MustParsePrefix("198.51.100.0/24"),
	netip.MustParsePrefix("203.0.113.0/24"),
	netip.MustParsePrefix("240.0.0.0/4"),
	netip.MustParsePrefix("64:ff9b::/96"),
	netip.MustParsePrefix("64:ff9b:1::/48"),
	netip.MustParsePrefix("100::/64"),
	netip.MustParsePrefix("2001::/23"),
	netip.MustParsePrefix("2001:db8::/32"),
	netip.MustParsePrefix("2002::/16"),
}

// IsPublic reports whether addr is a globally routable unicast address.
func IsPublic(addr netip.Addr) bool {
	addr = addr.Unmap()
	if !addr.IsValid() || !addr.IsGlobalUnicast() || addr.IsPrivate() {
		return false
	}
	for _, p := range nonPublicPrefixes {
		if p.Contains(addr) {
			return false
		}
	}
	return true
}

// Policy decides which addresses may be contacted.
type Policy struct {
	// AllowPrivate disables the check (tests and trusted environments).
	AllowPrivate bool
}

// Allowed reports whether addr may be contacted.
func (p Policy) Allowed(addr netip.Addr) bool {
	return p.AllowPrivate || IsPublic(addr)
}

// DialControl is a net.Dialer Control hook enforcing the policy on the
// resolved peer address of every connection attempt.
func (p Policy) DialControl(_, address string, _ syscall.RawConn) error {
	if p.AllowPrivate {
		return nil
	}
	ap, err := netip.ParseAddrPort(address)
	if err != nil || !IsPublic(ap.Addr()) {
		return ErrBlocked
	}
	return nil
}
