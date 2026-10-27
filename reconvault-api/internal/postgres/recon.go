package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/reconvault/reconvault-api/internal/recon"
)

// ReconRepository reads the tables written by reconvault-dns-autoscan.
type ReconRepository struct {
	pool *pgxpool.Pool
}

var _ recon.Reader = (*ReconRepository)(nil)

// NewReconRepository returns a repository backed by pool.
func NewReconRepository(pool *pgxpool.Pool) *ReconRepository {
	return &ReconRepository{pool: pool}
}

// undefinedTable reports PostgreSQL error 42P01: the autoscan service has
// not created its tables yet, which means there is no data rather than a
// server error.
func undefinedTable(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "42P01"
}

// Domain returns the domains row for hostname or recon.ErrNotFound.
func (r *ReconRepository) Domain(ctx context.Context, hostname string) (*recon.Domain, error) {
	var d recon.Domain
	err := r.pool.QueryRow(ctx, `
		SELECT hostname, root_domain, asn, asn_name, waf, updated_at
		FROM domains WHERE hostname = $1`, hostname).
		Scan(&d.Hostname, &d.RootDomain, &d.ASN, &d.ASNName, &d.WAF, &d.UpdatedAt)
	switch {
	case errors.Is(err, pgx.ErrNoRows), undefinedTable(err):
		return nil, recon.ErrNotFound
	case err != nil:
		return nil, fmt.Errorf("get domain: %w", err)
	}
	d.UpdatedAt = d.UpdatedAt.UTC()
	return &d, nil
}

// DNSHistory returns every stored DNS record of hostname, newest first.
// The history is append-only, so each version's rows share updated_at.
func (r *ReconRepository) DNSHistory(ctx context.Context, hostname string) ([]recon.DNSRecord, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT hostname, record_type, record_value, ttl, updated_at
		FROM dns_records
		WHERE hostname = $1
		ORDER BY updated_at DESC, record_type, record_value`, hostname)
	if undefinedTable(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("dns history: %w", err)
	}
	out, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (recon.DNSRecord, error) {
		var rec recon.DNSRecord
		err := row.Scan(&rec.Hostname, &rec.RecordType, &rec.RecordValue, &rec.TTL, &rec.UpdatedAt)
		rec.UpdatedAt = rec.UpdatedAt.UTC()
		return rec, err
	})
	if err != nil {
		return nil, fmt.Errorf("scan dns history: %w", err)
	}
	return out, nil
}

// Subdomains returns the distinct hostnames recorded under rootDomain.
func (r *ReconRepository) Subdomains(ctx context.Context, rootDomain string) ([]recon.Subdomain, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT DISTINCT hostname FROM domains
		WHERE root_domain = $1
		ORDER BY hostname`, rootDomain)
	if undefinedTable(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("subdomains: %w", err)
	}
	out, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (recon.Subdomain, error) {
		var s recon.Subdomain
		return s, row.Scan(&s.Hostname)
	})
	if err != nil {
		return nil, fmt.Errorf("scan subdomains: %w", err)
	}
	return out, nil
}

// RootDomains counts distinct hostnames per root domain, largest first.
func (r *ReconRepository) RootDomains(ctx context.Context, limit int) ([]recon.RootDomain, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT root_domain, count(DISTINCT hostname) AS hostname_count
		FROM domains
		GROUP BY root_domain
		ORDER BY hostname_count DESC, root_domain
		LIMIT $1`, limit)
	if undefinedTable(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("root domains: %w", err)
	}
	out, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (recon.RootDomain, error) {
		var d recon.RootDomain
		return d, row.Scan(&d.RootDomain, &d.HostnameCount)
	})
	if err != nil {
		return nil, fmt.Errorf("scan root domains: %w", err)
	}
	return out, nil
}
