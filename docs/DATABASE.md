# Database Design and Index Analysis

PostgreSQL 18 (any PostgreSQL ≥ 14 works). Migrations live in
`reconvault-api/internal/database/migrations/`, are embedded in the binary,
and run automatically at startup.

## Migrations

| Version | Contents |
| ------- | -------- |
| `0001_create_http_artifacts` | `http_artifacts` table |
| `0002_create_http_artifacts_indexes` | B-tree indexes for listing, filtering and the domain aggregation |
| `0003_create_http_artifacts_search` | `pg_trgm` extension, `http_artifact_search_document()` function, GIN trigram index |
| `0004_rename_response_body_to_response` | `response_body` renamed to `response`, which now holds the raw response (status line, headers, body) |

The runner:

- Applies the files in lexical order, each in its own transaction, and
  records them in `schema_migrations`.
- Holds a PostgreSQL advisory lock, so several replicas starting together
  are safe.
- Skips versions that are already applied, so restarts are no-ops.

To add a migration, create the next `NNNN_description.sql` file. Never edit
a migration that has already been applied in any environment.

`CREATE EXTENSION pg_trgm` needs a role allowed to create extensions (the
Compose database owner can). With a managed database where the app role
can't, have an administrator run `CREATE EXTENSION pg_trgm;` first. The
migration uses `IF NOT EXISTS`.

### Migration 0004: raw response

`response_body` became `response`, which stores the complete raw response
so response headers are recorded and searchable. The migration is only a
column rename, a catalog change that takes a brief lock:

- No data is rewritten. The trigram index refers to the column by
  position, so it stays valid without a rebuild.
- The search function's last parameter keeps its old name,
  `p_response_body`. Parameter names don't affect behavior, and recreating
  the function would force a full index rebuild.
- Rows recorded before 0004 keep their body-only content, because their
  headers were never stored. `TestMigrationKeepsExistingResponses` checks
  that such rows survive the upgrade and stay searchable.

### Tables owned by reconvault-dns-autoscan

`domains`, `dns_records`, `dns_scan_state`, `autoscan_jobs` and the view
`dns_records_current` are created and migrated by the DNS autoscan
service. They're tracked in `autoscan_schema_migrations`, separately from
this API's `schema_migrations`. The service only reads `http_artifacts`.
Its schema, the record versioning model and its indexes are documented in
[reconvault-dns-autoscan/README.md](../reconvault-dns-autoscan/README.md#database).

## Queries

All values are bind parameters. Sort identifiers come from a server-side
whitelist (`listSortColumns` / `domainSortColumns`) and never from raw
input.

```sql
-- POST /api/v1/http/request
INSERT INTO http_artifacts (http_method, hostname, endpoint_path, full_url, requested_at,
  response_status_code, response_content_type, response_body_size_bytes,
  request_headers, request_body, response)
VALUES ($1, ..., $11) RETURNING id;

-- GET /api/v1/http/artifacts (first page; LIMIT is page size + 1 to detect a next page)
SELECT id, http_method, full_url, response_status_code, response_content_type,
       response_body_size_bytes, requested_at
FROM http_artifacts
[WHERE hostname = $1]
ORDER BY <sort column> <ASC|DESC>, id <ASC|DESC>
LIMIT $n;

-- GET /api/v1/http/artifacts?cursor=... (next page, e.g. requested_at DESC)
... WHERE [hostname = $1 AND] (requested_at, id) < ($v, $id)
ORDER BY requested_at DESC, id DESC LIMIT $n;

-- GET /api/v1/http/artifacts/{id}
SELECT <all columns> FROM http_artifacts WHERE id = $1;

-- DELETE /api/v1/http/artifacts/{id}   (0 rows affected → 404)
DELETE FROM http_artifacts WHERE id = $1;

-- GET /api/v1/http/artifacts/search
SELECT id, http_method, hostname, endpoint_path, full_url, requested_at,
       response_status_code, response_content_type, response_body_size_bytes
FROM http_artifacts
WHERE http_artifact_search_document(id, http_method, ..., response) ILIKE $1  -- '%<escaped q>%'
ORDER BY requested_at DESC, id DESC
LIMIT $2;

-- GET /api/v1/http/domains
SELECT hostname, COUNT(*) AS artifact_count, MAX(requested_at) AS last_requested_at
FROM http_artifacts
GROUP BY hostname
ORDER BY <artifact_count | last_requested_at> <ASC|DESC>, hostname ASC
LIMIT $1;
```

The recon endpoints only read the tables owned by the DNS autoscan service.
If those tables don't exist yet (SQLSTATE `42P01`), they return "not found"
or an empty list instead of an error.

```sql
-- GET /api/v1/domains/{hostname}          (unique index on domains.hostname)
SELECT hostname, root_domain, asn, asn_name, waf, updated_at
FROM domains WHERE hostname = $1;

-- GET /api/v1/dns_records/{hostname}      (idx_dns_records_version, leading hostname)
SELECT hostname, record_type, record_value, ttl, updated_at
FROM dns_records WHERE hostname = $1
ORDER BY updated_at DESC, record_type, record_value;          -- no LIMIT: full history

-- GET /api/v1/subdomains/{root_domain}    (idx_domains_root_domain)
SELECT DISTINCT hostname FROM domains WHERE root_domain = $1 ORDER BY hostname;

-- GET /api/v1/root_domains
SELECT root_domain, count(DISTINCT hostname) AS hostname_count
FROM domains
GROUP BY root_domain
ORDER BY hostname_count DESC, root_domain
LIMIT $1;
```

## Indexes

| Index | Definition | Serves |
| ----- | ---------- | ------ |
| `http_artifacts_pkey` | `(id)` | `GET /api/v1/http/artifacts/{id}` |
| `idx_http_artifacts_requested_at` | `(requested_at DESC, id DESC)` | Default listing. Search ordering for frequent terms |
| `idx_http_artifacts_hostname_requested_at` | `(hostname, requested_at DESC, id DESC)` | `hostname=` filter with any sort. Index-only scan for `/api/v1/http/domains` |
| `idx_http_artifacts_response_status_code` | `(response_status_code, id)` | `sort_by=response_status_code` (both directions) |
| `idx_http_artifacts_response_body_size_bytes` | `(response_body_size_bytes, id)` | `sort_by=response_body_size_bytes` (both directions) |
| `idx_http_artifacts_http_method` | `(http_method, id)` | `sort_by=http_method` (both directions) |
| `idx_http_artifacts_search_trgm` | `GIN (http_artifact_search_document(...) gin_trgm_ops)` | Substring search |

B-tree indexes can be scanned backwards. That's why every index ends in
`id`, matching the `ORDER BY <col>, id` tie-breaker: one index serves both
`ASC` and `DESC`.

### Columns deliberately not B-tree indexed

`full_url` and `response_content_type` are unbounded `TEXT` values that the
caller or the upstream server controls. A B-tree entry larger than about
2.7 kB makes the **INSERT fail**, which would lose the artifact. Sorting
by these columns without a hostname filter therefore uses a parallel
sequential scan with a top-N heapsort (about 40 ms at 200k rows, linear in
table size). For the same reason the API limits hostnames to the DNS
maximum of 253 characters before they reach
`idx_http_artifacts_hostname_requested_at`. A regression test inserts
maximum-size values.

### Full-field search design

The spec asks for case-insensitive substring search over every column.
Separate trigram indexes per column don't work for the non-text columns
(`id`, timestamps, integers), and an `OR` over twelve predicates plans
badly. Instead, one `IMMUTABLE` SQL function concatenates all columns
into a single search document, and one GIN trigram index covers that
expression:

- Fields are joined with U+001F (unit separator). The API rejects control
  characters in `q`, so a match can never span two fields (there's a test
  for this).
- `requested_at` is rendered in UTC with a fixed pattern
  (`YYYY-MM-DD"T"HH24:MI:SS.US"Z"`). That makes the function genuinely
  independent of `TimeZone`, `DateStyle` and locale, which is what makes
  `IMMUTABLE` correct here.
- The query applies `ILIKE` to the exact indexed expression. `%`, `_` and
  `\` in `q` are escaped, so user input is always matched literally.
- Nothing is duplicated in the heap: the expression index stores only
  trigrams. A stored generated column would double the size of every body.

The query must call the function with exactly the arguments the index
uses. `TestSearchUsesTrigramIndex` guards this by asserting the plan uses
the index.

## EXPLAIN (ANALYZE, BUFFERS) results

Dataset: 200,000 rows, 2,000 hostnames, about 600 B response bodies plus
JSON headers. The table is 195 MB, the trigram index 125 MB and the
hostname index 16 MB. PostgreSQL 18, Docker, warm cache.

| Query | Plan | Time |
| ----- | ---- | ---- |
| List, default (`requested_at DESC`, limit 30) | Index Scan `idx_http_artifacts_requested_at` | 0.17 ms |
| List, `hostname=` + default sort | Index Scan `idx_http_artifacts_hostname_requested_at` | 0.36 ms |
| List, `hostname=` + `sort_by=response_status_code` | Bitmap Index Scan (hostname) → top-N sort of ~100 rows | 0.77 ms |
| List, `sort_by=response_status_code` (no index) | Parallel Seq Scan → top-N heapsort | 42 ms |
| List, `sort_by=response_status_code` ASC / DESC | Index Only Scan / Backward `idx_..._response_status_code` | 0.06 / 0.05 ms |
| List, `sort_by=response_body_size_bytes DESC` | Index Only Scan Backward `idx_..._response_body_size_bytes` | 0.05 ms |
| List, `sort_by=http_method` | Index Only Scan `idx_http_artifacts_http_method` | 0.08 ms |
| Domains (before VACUUM) | Parallel Seq Scan → HashAggregate → top-N | 43 ms |
| Domains (after VACUUM) | Parallel Index Only Scan `idx_..._hostname_requested_at`, Heap Fetches: 0 → GroupAggregate | 30 ms |
| Search, rare term (32-char hash, 1 hit) | Bitmap Index Scan `idx_http_artifacts_search_trgm` | 7.5 ms |
| Search, frequent term (`lorem`, in every row) | Index Scan `idx_http_artifacts_requested_at` + filter, stops after 30 hits | 1.1 ms |

The status/size/method rows were measured before and after creating the
indexes. Each cut the query from about 42 ms to under 0.1 ms, roughly
500×. The planner picks the right search strategy on its own: the trigram
index for selective terms, and a newest-first walk with early exit for
common terms.

### Keyset pagination

`GET /api/v1/http/artifacts` pages with a cursor holding the last row's
`(sort value, id)`. The next page starts strictly after it, using a row
comparison such as `(col, id) > ($v, $id)`. That is an index range
condition on every `(col, id)` index, so each page costs the same at any
depth. `OFFSET` would re-read every skipped row, and its pages shift as
new artifacts arrive.

NULLs need care in the nullable sort columns (`response_status_code`,
`response_content_type`, `response_body_size_bytes`). They sort last for
`ASC` and first for `DESC`, and `(NULL, id) > (...)` is never true. When
the remaining rows span both a non-NULL range and the NULL group, the
naive `(col, id) > ($v, $id) OR col IS NULL` can't bound an index scan:
PostgreSQL scans from the start of the index and filters. Instead, the
query uses two index-bounded segments:

```sql
SELECT * FROM (
  (SELECT ... WHERE (response_status_code, id) > ($v, $id) ORDER BY response_status_code, id LIMIT $n)
  UNION ALL
  (SELECT ... WHERE response_status_code IS NULL           ORDER BY response_status_code, id LIMIT $n)
) page ORDER BY response_status_code, id LIMIT $n;
```

Measured on 200k rows (4,000 NULL status codes):

| Next-page query | Plan | Time |
| --------------- | ---- | ---- |
| `requested_at DESC`, cursor deep in the table | Index Only Scan, row-comparison bound | 0.12 ms |
| `hostname=` + `requested_at DESC` cursor | Index Only Scan `idx_..._hostname_requested_at` | 0.05 ms |
| `response_status_code ASC`, `OR IS NULL` form (rejected) | Index scan from the start + filter | 6.9 ms, grows with depth |
| `response_status_code ASC`, two segments (used) | Two bounded index scans | 0.6 ms (full rows) |
| `response_status_code DESC`, inside the NULL group (two segments) | Two bounded backward index scans | 0.4 ms |
| `response_body_size_bytes ASC`, inside the NULL group | Index Only Scan, `IS NULL AND id > $id` | 0.04 ms |
| `DELETE ... WHERE id = $1` | Index Scan `http_artifacts_pkey` | 0.3 ms |

`TestListPagination` walks every sort field and direction, with and
without a hostname filter and at several page sizes, over data with
NULLs and duplicate sort values. It checks that the pages concatenate to
exactly the unpaginated result.

### Search always uses custom plans

pgx caches prepared statements by default. After five executions,
PostgreSQL may switch a prepared statement to a *generic* plan that
ignores the parameter value. For search, that's dangerous: whether the
trigram index or a `requested_at` walk is better depends entirely on how
common the term is. Measured with `plan_cache_mode = force_generic_plan`:

| Term | Custom plan | Generic plan |
| ---- | ----------- | ------------ |
| `lorem` (in every row) | 0.6 ms: `requested_at` walk, stops at LIMIT | **2,579 ms**: bitmap scan + recheck of all 200k rows |
| 32-char hash (1 row) | 2.5 ms: trigram bitmap scan | 4.3 ms |

The repository therefore runs the search with `pgx.QueryExecModeExec`,
an unnamed statement that PostgreSQL always plans with the actual
pattern. Over the API, with 20 alternating rare and common searches, the
latency stayed flat (rare about 6 ms, common about 3 ms end to end). The
other queries filter only by equality or have no parameters except
`LIMIT`, so cached generic plans are safe for them.

### Notes and limits

- **Domains cost grows with the table.** The query has to aggregate every
  row. The hostname index makes it an index-only scan, which needs an
  up-to-date visibility map (autovacuum maintains it). With many millions
  of rows, a summary table maintained by trigger would be the next step.
  It isn't implemented because the spec doesn't require it.
- **Short search terms** (1–2 characters) produce no trigrams, so the index
  can't narrow them. They are still correct and bounded by `LIMIT`.
- **Write cost.** The trigram index dominates insert cost and index size.
  This is the price of full-body substring search. Outbound HTTP latency
  still dominates `POST /api/v1/http/request`.

### Reproducing the analysis

```sh
docker compose exec db psql -U reconvault -d reconvault
```

```sql
EXPLAIN (ANALYZE, BUFFERS)
SELECT id FROM http_artifacts
WHERE http_artifact_search_document(id, http_method, hostname, endpoint_path, full_url,
        requested_at, response_status_code, response_content_type, response_body_size_bytes,
        request_headers, request_body, response) ILIKE '%secret%'
ORDER BY requested_at DESC, id DESC LIMIT 30;
```
