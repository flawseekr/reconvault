-- Tables owned by reconvault-dns-autoscan. http_artifacts is owned and
-- migrated by reconvault-api and is only read here.

-- One row per scanned hostname (schema as specified).
CREATE TABLE domains (
    hostname TEXT NOT NULL UNIQUE,
    root_domain TEXT NOT NULL,
    asn BIGINT,
    asn_name TEXT,
    waf BOOLEAN,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_domains_root_domain ON domains (root_domain);

-- Append-only history of DNS record sets (schema as specified). Each
-- version of a (hostname, record_type) record set is inserted as rows that
-- share one updated_at; unchanged sets are never re-inserted.
CREATE TABLE dns_records (
    hostname TEXT NOT NULL,
    record_type VARCHAR(10) NOT NULL,
    record_value TEXT NOT NULL,
    ttl INTEGER,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Version lookups: latest set per (hostname, record_type).
CREATE INDEX idx_dns_records_version ON dns_records (hostname, record_type, updated_at DESC);

-- No duplicate values within one version. record_value is hashed because
-- TXT values can exceed the B-tree entry size limit.
CREATE UNIQUE INDEX uq_dns_records_version_value
    ON dns_records (hostname, record_type, updated_at, md5(record_value));

-- Outcome of the latest lookup per (hostname, record_type), kept apart from
-- the record data so a failed query is never mistaken for an absent record.
CREATE TABLE dns_scan_state (
    hostname TEXT NOT NULL,
    record_type VARCHAR(10) NOT NULL,
    -- found: records exist; nodata/nxdomain: confirmed absent; error: lookup failed.
    status VARCHAR(16) NOT NULL CHECK (status IN ('found', 'nodata', 'nxdomain', 'error')),
    -- updated_at of the current record set in dns_records; NULL when no
    -- records are currently known (confirmed absent, or never found).
    current_version TIMESTAMPTZ,
    last_attempt_at TIMESTAMPTZ NOT NULL,
    -- Last conclusive answer (found, nodata or nxdomain).
    last_success_at TIMESTAMPTZ,
    last_error TEXT,
    consecutive_failures INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (hostname, record_type)
);

-- Bookkeeping for scheduled jobs (daily refresh catch-up after downtime).
CREATE TABLE autoscan_jobs (
    name TEXT PRIMARY KEY,
    last_started_at TIMESTAMPTZ,
    last_completed_at TIMESTAMPTZ
);

-- The currently valid records: the latest version of each record set that
-- the most recent conclusive lookup still confirmed.
CREATE VIEW dns_records_current AS
SELECT r.hostname, r.record_type, r.record_value, r.ttl, r.updated_at
FROM dns_records r
JOIN dns_scan_state s
  ON s.hostname = r.hostname
 AND s.record_type = r.record_type
 AND s.current_version = r.updated_at;
