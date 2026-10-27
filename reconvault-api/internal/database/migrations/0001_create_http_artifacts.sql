-- http_artifacts table: one row per recorded HTTP request/response.
-- gen_random_uuid() is built into PostgreSQL 13+.
CREATE TABLE http_artifacts (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    http_method VARCHAR(16) NOT NULL,
    hostname TEXT NOT NULL,
    endpoint_path TEXT NOT NULL,
    full_url TEXT NOT NULL,
    requested_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    response_status_code INTEGER,
    response_content_type TEXT,
    response_body_size_bytes BIGINT,
    request_headers JSONB,
    request_body TEXT,
    response_body TEXT
);
