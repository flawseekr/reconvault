package domainscan

import (
	"net/netip"
	"strings"

	"golang.org/x/net/publicsuffix"
)

// RootDomain returns the registrable domain of host using the ICANN
// section of the Public Suffix List, so multi-level public suffixes are
// handled ("a.b.example.co.uk" -> "example.co.uk"). Private-section
// entries such as "github.io" are not treated as suffixes
// ("user.github.io" -> "github.io"), matching the common definition of a
// root (registrable) domain. IP literals and names without a registrable
// part (e.g. "localhost", "co.uk") are returned unchanged.
// Unlisted TLDs follow the PSL's implicit "*" rule ("a.b.invalid" ->
// "b.invalid").
func RootDomain(host string) string {
	host = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(host)), ".")
	if _, err := netip.ParseAddr(host); err == nil || host == "" {
		return host
	}
	suffix := icannSuffix(host)
	if suffix == "" || suffix == host {
		return host
	}
	rest := strings.TrimSuffix(host, "."+suffix)
	if rest == host {
		return host
	}
	if i := strings.LastIndexByte(rest, '.'); i >= 0 {
		rest = rest[i+1:]
	}
	return rest + "." + suffix
}

// icannSuffix returns the longest ICANN public suffix of host. When the
// list matches a private-section rule, it walks up to the enclosing ICANN
// suffix ("github.io" -> "io"). When no ICANN rule matches, the PSL's
// implicit "*" rule applies: the last label is the suffix.
func icannSuffix(host string) string {
	suffix, icann := publicsuffix.PublicSuffix(host)
	for !icann {
		_, parent, ok := strings.Cut(suffix, ".")
		if !ok {
			return lastLabel(host)
		}
		suffix, icann = publicsuffix.PublicSuffix(parent)
	}
	return suffix
}

func lastLabel(host string) string {
	if i := strings.LastIndexByte(host, '.'); i >= 0 {
		return host[i+1:]
	}
	return host
}
