// Package store persists scan results in PostgreSQL.
package store

import (
	"context"
	"errors"
	"fmt"
	"hash/fnv"
	"slices"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/reconvault/reconvault-dns-autoscan/internal/dnsscan"
	"github.com/reconvault/reconvault-dns-autoscan/internal/domainscan"
)

// maxErrorLength bounds stored lookup error messages.
const maxErrorLength = 500

// Store is safe for concurrent use.
type Store struct {
	pool *pgxpool.Pool
}

// New returns a Store.
func New(pool *pgxpool.Pool) *Store { return &Store{pool: pool} }

// PendingDomainHosts returns hostnames recorded in http_artifacts that have
// no domains row yet.
func (s *Store) PendingDomainHosts(ctx context.Context, limit int) ([]string, error) {
	return s.strings(ctx, `
		SELECT DISTINCT a.hostname
		FROM http_artifacts a
		WHERE NOT EXISTS (SELECT 1 FROM domains d WHERE d.hostname = a.hostname)
		ORDER BY a.hostname
		LIMIT $1`, limit)
}

// PendingDNSTargets returns hostnames and root domains from domains that
// have never been DNS-scanned. A target counts as scanned once any lookup
// outcome is recorded in dns_scan_state, so names confirmed to have no
// records (NXDOMAIN) are not rescanned on every active pass.
func (s *Store) PendingDNSTargets(ctx context.Context, limit int) ([]string, error) {
	return s.strings(ctx, `
		SELECT t.hostname
		FROM (SELECT hostname FROM domains UNION SELECT root_domain FROM domains) t
		WHERE NOT EXISTS (SELECT 1 FROM dns_scan_state s WHERE s.hostname = t.hostname)
		ORDER BY t.hostname
		LIMIT $1`, limit)
}

// ScannedDNSTargets pages through every DNS-scanned hostname after the
// given one (keyset pagination).
func (s *Store) ScannedDNSTargets(ctx context.Context, after string, limit int) ([]string, error) {
	return s.strings(ctx, `
		SELECT DISTINCT hostname FROM dns_scan_state
		WHERE hostname > $1
		ORDER BY hostname
		LIMIT $2`, after, limit)
}

// ScannedDomains pages through every hostname in domains after the given
// one (keyset pagination), for the daily root domain, ASN and WAF refresh.
func (s *Store) ScannedDomains(ctx context.Context, after string, limit int) ([]string, error) {
	return s.strings(ctx, `
		SELECT hostname FROM domains
		WHERE hostname > $1
		ORDER BY hostname
		LIMIT $2`, after, limit)
}

func (s *Store) strings(ctx context.Context, sql string, args ...any) ([]string, error) {
	rows, err := s.pool.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowTo[string])
}

// UpsertDomain inserts or refreshes a domains row. A rescan that cannot
// determine a value keeps the previously verified one instead of erasing
// it; asn and asn_name are always taken together so they never describe
// different ASNs.
func (s *Store) UpsertDomain(ctx context.Context, d domainscan.Domain) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO domains (hostname, root_domain, asn, asn_name, waf, updated_at)
		VALUES ($1, $2, $3, $4, $5, NOW())
		ON CONFLICT (hostname) DO UPDATE SET
			root_domain = EXCLUDED.root_domain,
			asn      = CASE WHEN EXCLUDED.asn IS NULL THEN domains.asn ELSE EXCLUDED.asn END,
			asn_name = CASE WHEN EXCLUDED.asn IS NULL THEN domains.asn_name ELSE EXCLUDED.asn_name END,
			waf      = COALESCE(EXCLUDED.waf, domains.waf),
			updated_at = NOW()`,
		d.Hostname, d.RootDomain, d.ASN, d.ASNName, d.WAF)
	if err != nil {
		return fmt.Errorf("upsert domain %s: %w", d.Hostname, err)
	}
	return nil
}

// ApplyResult is a summary of ApplyDNSOutcomes for logging.
type ApplyResult struct {
	NewVersions int
	Unchanged   int
	Absent      int
	Failed      int
}

// ApplyDNSOutcomes records one host's lookup outcomes in a transaction.
//
// dns_records is append-only: a record set is inserted as a new version
// (rows sharing updated_at = now) only when it differs from the current
// version; TTL-only changes do not create versions. Confirmed absence
// (NXDOMAIN/NODATA) clears the current version without deleting history.
// A failed lookup keeps the current version, so valid records are never
// dropped because of a transient error.
func (s *Store) ApplyDNSOutcomes(ctx context.Context, host string, outcomes []dnsscan.Outcome, now time.Time) (ApplyResult, error) {
	now = now.UTC().Truncate(time.Microsecond) // PostgreSQL precision: versions compare exactly
	var res ApplyResult
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		for _, o := range outcomes {
			if err := applyOutcome(ctx, tx, host, o, now, &res); err != nil {
				return fmt.Errorf("%s %s: %w", host, o.Type, err)
			}
		}
		return nil
	})
	return res, err
}

type scanState struct {
	exists         bool
	currentVersion *time.Time
	lastSuccess    *time.Time
	failures       int
}

func applyOutcome(ctx context.Context, tx pgx.Tx, host string, o dnsscan.Outcome, now time.Time, res *ApplyResult) error {
	var st scanState
	err := tx.QueryRow(ctx, `
		SELECT current_version, last_success_at, consecutive_failures
		FROM dns_scan_state WHERE hostname = $1 AND record_type = $2
		FOR UPDATE`, host, o.Type).Scan(&st.currentVersion, &st.lastSuccess, &st.failures)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
	case err != nil:
		return err
	default:
		st.exists = true
	}

	next := struct {
		version     *time.Time
		lastSuccess *time.Time
		lastError   *string
		failures    int
	}{version: st.currentVersion, lastSuccess: st.lastSuccess}

	switch o.Status {
	case dnsscan.StatusFound:
		same, err := sameAsCurrent(ctx, tx, host, o, st.currentVersion)
		if err != nil {
			return err
		}
		if same {
			res.Unchanged++
		} else {
			if err := insertVersion(ctx, tx, host, o, now); err != nil {
				return err
			}
			next.version = &now
			res.NewVersions++
		}
		next.lastSuccess = &now
	case dnsscan.StatusNoData, dnsscan.StatusNXDomain:
		next.version = nil
		next.lastSuccess = &now
		res.Absent++
	case dnsscan.StatusError:
		msg := "lookup failed"
		if o.Err != nil {
			msg = o.Err.Error()
		}
		if len(msg) > maxErrorLength {
			msg = msg[:maxErrorLength]
		}
		next.lastError = &msg
		next.failures = st.failures + 1
		res.Failed++
	default:
		return fmt.Errorf("unknown status %q", o.Status)
	}

	_, err = tx.Exec(ctx, `
		INSERT INTO dns_scan_state
			(hostname, record_type, status, current_version, last_attempt_at, last_success_at, last_error, consecutive_failures)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		ON CONFLICT (hostname, record_type) DO UPDATE SET
			status = EXCLUDED.status,
			current_version = EXCLUDED.current_version,
			last_attempt_at = EXCLUDED.last_attempt_at,
			last_success_at = EXCLUDED.last_success_at,
			last_error = EXCLUDED.last_error,
			consecutive_failures = EXCLUDED.consecutive_failures`,
		host, o.Type, string(o.Status), next.version, now, next.lastSuccess, next.lastError, next.failures)
	return err
}

// sameAsCurrent compares the observed values with the current version.
func sameAsCurrent(ctx context.Context, tx pgx.Tx, host string, o dnsscan.Outcome, version *time.Time) (bool, error) {
	if version == nil {
		return false, nil
	}
	rows, err := tx.Query(ctx, `
		SELECT record_value FROM dns_records
		WHERE hostname = $1 AND record_type = $2 AND updated_at = $3
		ORDER BY record_value`, host, o.Type, *version)
	if err != nil {
		return false, err
	}
	current, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return false, err
	}
	observed := make([]string, len(o.Records))
	for i, r := range o.Records {
		observed[i] = r.Value
	}
	slices.Sort(current)
	slices.Sort(observed)
	return slices.Equal(current, observed), nil
}

func insertVersion(ctx context.Context, tx pgx.Tx, host string, o dnsscan.Outcome, now time.Time) error {
	values := make([]string, len(o.Records))
	ttls := make([]int64, len(o.Records))
	for i, r := range o.Records {
		values[i] = r.Value
		ttls[i] = int64(r.TTL) // RFC 2181: 0 .. 2^31-1
	}
	_, err := tx.Exec(ctx, `
		INSERT INTO dns_records (hostname, record_type, record_value, ttl, updated_at)
		SELECT $1, $2, v, t::integer, $5
		FROM unnest($3::text[], $4::bigint[]) AS x(v, t)`,
		host, o.Type, values, ttls, now)
	return err
}

// LastCompleted returns when the job last completed, or nil.
func (s *Store) LastCompleted(ctx context.Context, job string) (*time.Time, error) {
	var t *time.Time
	err := s.pool.QueryRow(ctx, "SELECT last_completed_at FROM autoscan_jobs WHERE name = $1", job).Scan(&t)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return t, err
}

// JobStarted records the start of a job run.
func (s *Store) JobStarted(ctx context.Context, job string, at time.Time) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO autoscan_jobs (name, last_started_at) VALUES ($1, $2)
		ON CONFLICT (name) DO UPDATE SET last_started_at = EXCLUDED.last_started_at`, job, at)
	return err
}

// JobCompleted records the completion of a job run.
func (s *Store) JobCompleted(ctx context.Context, job string, at time.Time) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO autoscan_jobs (name, last_completed_at) VALUES ($1, $2)
		ON CONFLICT (name) DO UPDATE SET last_completed_at = EXCLUDED.last_completed_at`, job, at)
	return err
}

// TryLock takes a session-level advisory lock for name on a dedicated
// connection, so only one replica runs a job at a time. ok is false when
// another session holds it.
func (s *Store) TryLock(ctx context.Context, name string) (unlock func(), ok bool, err error) {
	conn, err := s.pool.Acquire(ctx)
	if err != nil {
		return nil, false, err
	}
	key := lockKey(name)
	if err := conn.QueryRow(ctx, "SELECT pg_try_advisory_lock($1)", key).Scan(&ok); err != nil || !ok {
		conn.Release()
		return nil, false, err
	}
	return func() {
		unlockCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, _ = conn.Exec(unlockCtx, "SELECT pg_advisory_unlock($1)", key)
		conn.Release()
	}, true, nil
}

func lockKey(name string) int64 {
	h := fnv.New64a()
	_, _ = h.Write([]byte("reconvault-dns-autoscan:" + name))
	return int64(h.Sum64())
}

// Ping checks database connectivity.
func (s *Store) Ping(ctx context.Context) error { return s.pool.Ping(ctx) }
