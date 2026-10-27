/**
 * Types mirroring the ReconVault API contract
 * (reconvault-api/api/swagger.json, OpenAPI 3.0).
 */

/** ISO 8601 / RFC 3339 timestamp in UTC. */
export type Timestamp = string;

export type SortOrder = "ASC" | "DESC";

export const ARTIFACT_SORT_FIELDS = [
  "http_method",
  "full_url",
  "response_status_code",
  "response_content_type",
  "response_body_size_bytes",
  "requested_at",
] as const;
export type ArtifactSortField = (typeof ARTIFACT_SORT_FIELDS)[number];

export const DOMAIN_SORT_FIELDS = ["total_artifacts", "latest_update"] as const;
export type DomainSortField = (typeof DOMAIN_SORT_FIELDS)[number];

/** Methods accepted by POST /api/v1/http/request. */
export const HTTP_METHODS = ["GET", "HEAD", "POST", "PUT", "PATCH", "DELETE", "OPTIONS", "TRACE"] as const;
export type HttpMethod = (typeof HTTP_METHODS)[number];

/** GET /api/v1/http/artifacts list item. */
export interface ArtifactSummary {
  id: string;
  http_method: string;
  full_url: string;
  response_status_code: number | null;
  response_content_type: string | null;
  response_body_size_bytes: number | null;
  requested_at: Timestamp;
}

/** GET /api/v1/http/artifacts/search result: metadata only. */
export interface SearchResult extends ArtifactSummary {
  hostname: string;
  endpoint_path: string;
}

/** GET /api/v1/http/artifacts/{id}: every column. */
export interface Artifact extends SearchResult {
  request_headers: Record<string, string> | null;
  request_body: string | null;
  /** Raw HTTP response: status line, headers, blank line, body. */
  response: string | null;
}

/** POST /api/v1/http/request 201 body. */
export interface ExecuteResponse extends Artifact {
  response_body_truncated: boolean;
}

export interface ExecuteRequest {
  method: HttpMethod;
  url: string;
  headers?: Record<string, string> | null;
  body?: string | null;
}

export interface Domain {
  hostname: string;
  artifact_count: number;
  last_requested_at: Timestamp;
}

export interface ArtifactListMeta {
  sort_by: ArtifactSortField;
  sort_order: SortOrder;
  hostname: string | null;
  limit: number;
  next_cursor: string | null;
}

export interface SearchMeta {
  query: string;
  limit: number;
}

export interface DomainMeta {
  sort_by: DomainSortField;
  sort_order: SortOrder;
  limit: number;
}

export interface ListResponse<T, M> {
  data: T[];
  meta: M;
}

export type ArtifactList = ListResponse<ArtifactSummary, ArtifactListMeta>;
export type SearchResponse = ListResponse<SearchResult, SearchMeta>;
export type DomainList = ListResponse<Domain, DomainMeta>;

/** GET /api/v1/domains/{hostname}; null fields are not known yet. */
export interface DomainInfo {
  hostname: string;
  root_domain: string;
  asn: number | null;
  asn_name: string | null;
  waf: boolean | null;
  updated_at: Timestamp;
}

/** One stored DNS record version (GET /api/v1/dns_records/{hostname}). */
export interface DnsRecord {
  hostname: string;
  record_type: string;
  record_value: string;
  ttl: number | null;
  updated_at: Timestamp;
}

export interface RootDomain {
  root_domain: string;
  hostname_count: number;
}

export type DnsRecordList = ListResponse<DnsRecord, { hostname: string; total: number }>;
export type SubdomainList = ListResponse<{ hostname: string }, { root_domain: string; total: number }>;
export type RootDomainList = ListResponse<RootDomain, { limit: number }>;

/** Error envelope returned by every failing endpoint. */
export interface ApiErrorBody {
  error: {
    code: string;
    message: string;
    field?: string;
    artifact_id?: string;
  };
}

/** API limits (see docs/API.md). */
export const LIMITS = {
  listMax: 100,
  searchMax: 100,
  domainsMax: 1000,
  queryMaxBytes: 1024,
  urlMaxLength: 8192,
  hostnameMaxLength: 255,
} as const;
