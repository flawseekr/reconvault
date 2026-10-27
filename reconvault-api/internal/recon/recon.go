// Package recon exposes the domain and DNS data collected by
// reconvault-dns-autoscan (tables domains and dns_records). The API only
// reads these tables; the autoscan service owns and writes them.
package recon

import (
	"context"
	"errors"
	"time"
)

// ErrNotFound is returned when a hostname has no domains row.
var ErrNotFound = errors.New("domain not found")

// Domain is a row of the domains table.
type Domain struct {
	Hostname   string    `json:"hostname"`
	RootDomain string    `json:"root_domain"`
	ASN        *int64    `json:"asn"`
	ASNName    *string   `json:"asn_name"`
	WAF        *bool     `json:"waf"`
	UpdatedAt  time.Time `json:"updated_at"`
}

// DNSRecord is a row of the dns_records history.
type DNSRecord struct {
	Hostname    string    `json:"hostname"`
	RecordType  string    `json:"record_type"`
	RecordValue string    `json:"record_value"`
	TTL         *int32    `json:"ttl"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// Subdomain is a hostname under a root domain.
type Subdomain struct {
	Hostname string `json:"hostname"`
}

// RootDomain aggregates the hostnames scanned under a root domain.
type RootDomain struct {
	RootDomain    string `json:"root_domain"`
	HostnameCount int64  `json:"hostname_count"`
}

// Reader queries the autoscan tables.
type Reader interface {
	Domain(ctx context.Context, hostname string) (*Domain, error)
	DNSHistory(ctx context.Context, hostname string) ([]DNSRecord, error)
	Subdomains(ctx context.Context, rootDomain string) ([]Subdomain, error)
	RootDomains(ctx context.Context, limit int) ([]RootDomain, error)
}

// Result-size limits of RootDomains, matching GET /api/v1/http/domains.
const (
	DefaultRootDomainLimit = 100
	MaxRootDomainLimit     = 1000
)
