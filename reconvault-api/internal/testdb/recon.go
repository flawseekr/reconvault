package testdb

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

// reconSchema mirrors the tables reconvault-dns-autoscan creates (its
// migration 0001), which the API reads but does not migrate.
const reconSchema = `
CREATE TABLE IF NOT EXISTS domains (
    hostname TEXT NOT NULL UNIQUE,
    root_domain TEXT NOT NULL,
    asn BIGINT,
    asn_name TEXT,
    waf BOOLEAN,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE TABLE IF NOT EXISTS dns_records (
    hostname TEXT NOT NULL,
    record_type VARCHAR(10) NOT NULL,
    record_value TEXT NOT NULL,
    ttl INTEGER,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
TRUNCATE domains, dns_records;`

// WithReconTables creates (or empties) the autoscan-owned tables.
func WithReconTables(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	if _, err := pool.Exec(context.Background(), reconSchema); err != nil {
		t.Fatalf("create recon tables: %v", err)
	}
}
