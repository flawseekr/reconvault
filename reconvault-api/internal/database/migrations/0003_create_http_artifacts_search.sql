-- Full-field, case-insensitive substring search for
-- GET /api/v1/http/artifacts/search, backed by a single GIN trigram index.

CREATE EXTENSION IF NOT EXISTS pg_trgm;

-- Builds the searchable document of an artifact from every column.
-- Fields are joined with the ASCII unit separator (U+001F), which the API
-- rejects in search queries, so a match can never span two fields.
--
-- Declared IMMUTABLE so it can back an expression index. That is safe here:
-- requested_at is rendered in UTC with a fixed, locale-independent pattern,
-- so the result does not depend on TimeZone, DateStyle or lc_* settings.
-- The timestamp format matches the RFC 3339 form returned by the API.
CREATE FUNCTION http_artifact_search_document(
    p_id UUID,
    p_http_method TEXT,
    p_hostname TEXT,
    p_endpoint_path TEXT,
    p_full_url TEXT,
    p_requested_at TIMESTAMPTZ,
    p_response_status_code INTEGER,
    p_response_content_type TEXT,
    p_response_body_size_bytes BIGINT,
    p_request_headers JSONB,
    p_request_body TEXT,
    p_response_body TEXT
) RETURNS TEXT
LANGUAGE sql
IMMUTABLE
PARALLEL SAFE
RETURN
    p_id::TEXT || chr(31) ||
    p_http_method || chr(31) ||
    p_hostname || chr(31) ||
    p_endpoint_path || chr(31) ||
    p_full_url || chr(31) ||
    to_char(p_requested_at AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS.US"Z"') || chr(31) ||
    COALESCE(p_response_status_code::TEXT, '') || chr(31) ||
    COALESCE(p_response_content_type, '') || chr(31) ||
    COALESCE(p_response_body_size_bytes::TEXT, '') || chr(31) ||
    COALESCE(p_request_headers::TEXT, '') || chr(31) ||
    COALESCE(p_request_body, '') || chr(31) ||
    COALESCE(p_response_body, '');

-- Any change to this expression must be mirrored in the search query in
-- internal/postgres/artifacts.go, otherwise the index will not be used.
CREATE INDEX idx_http_artifacts_search_trgm
    ON http_artifacts
    USING GIN (http_artifact_search_document(
        id, http_method, hostname, endpoint_path, full_url, requested_at,
        response_status_code, response_content_type, response_body_size_bytes,
        request_headers, request_body, response_body
    ) gin_trgm_ops);
