-- B-tree indexes derived from the query patterns of GET /api/v1/http/artifacts
-- and GET /api/v1/http/domains (see docs/DATABASE.md for the EXPLAIN analysis).

-- Default listing: ORDER BY requested_at DESC, id DESC LIMIT n.
-- Also used by the search endpoint's ordering.
CREATE INDEX idx_http_artifacts_requested_at
    ON http_artifacts (requested_at DESC, id DESC);

-- Listing filtered by hostname (any sort order), and the per-hostname
-- COUNT(*)/MAX(requested_at) aggregation of GET /api/v1/http/domains, which can
-- be answered with an index-only scan.
CREATE INDEX idx_http_artifacts_hostname_requested_at
    ON http_artifacts (hostname, requested_at DESC, id DESC);

-- Unfiltered sorts on bounded columns: ORDER BY <col> ASC|DESC, id ASC|DESC
-- becomes an (index-only) forward or backward scan instead of a full
-- sequential scan + top-N sort.
CREATE INDEX idx_http_artifacts_response_status_code
    ON http_artifacts (response_status_code, id);

CREATE INDEX idx_http_artifacts_response_body_size_bytes
    ON http_artifacts (response_body_size_bytes, id);

CREATE INDEX idx_http_artifacts_http_method
    ON http_artifacts (http_method, id);

-- full_url and response_content_type are deliberately not B-tree indexed:
-- they are unbounded, upstream-influenced TEXT values, and a B-tree entry
-- larger than ~2.7 kB makes the INSERT fail. Sorting by them falls back to
-- a sequential scan + top-N heapsort (see docs/DATABASE.md).
