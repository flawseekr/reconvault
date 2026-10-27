-- Store the raw HTTP response (status line, headers, blank line, body)
-- instead of the body alone, so response headers are recorded and
-- searchable. Rows recorded before this migration keep their body-only
-- content: their headers were never stored and cannot be reconstructed.
--
-- A rename is a catalog-only change: idx_http_artifacts_search_trgm
-- references the column by position and stays valid without a rebuild.
ALTER TABLE http_artifacts RENAME COLUMN response_body TO response;

COMMENT ON COLUMN http_artifacts.response IS
    'Raw HTTP response: status line, headers (canonical names, sorted), CRLF, body (truncated at MAX_RESPONSE_BODY_BYTES)';
