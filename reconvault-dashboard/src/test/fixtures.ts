import type { Artifact, ArtifactSummary, DnsRecord, Domain, DomainInfo } from "@/types/api";

export function summary(overrides: Partial<ArtifactSummary> = {}): ArtifactSummary {
  return {
    id: "550e8400-e29b-41d4-a716-446655440000",
    http_method: "GET",
    full_url: "https://api.example.com/v1/resource",
    response_status_code: 200,
    response_content_type: "application/json",
    response_body_size_bytes: 1536,
    requested_at: "2026-10-27T12:30:00.123456Z",
    ...overrides,
  };
}

export function artifact(overrides: Partial<Artifact> = {}): Artifact {
  return {
    ...summary(),
    hostname: "api.example.com",
    endpoint_path: "/v1/resource",
    request_headers: { "Content-Type": "application/json", "User-Agent": "ua/1" },
    request_body: '{"id":123}',
    response: 'HTTP/1.1 200 OK\r\nContent-Type: application/json\r\nX-Token: hdr_secret\r\n\r\n{"success":true}',
    ...overrides,
  };
}

export function domain(overrides: Partial<Domain> = {}): Domain {
  return { hostname: "api.example.com", artifact_count: 3, last_requested_at: "2026-10-27T12:30:00Z", ...overrides };
}

export const listMeta = {
  sort_by: "requested_at",
  sort_order: "DESC",
  hostname: null,
  limit: 50,
  next_cursor: null,
} as const;

export function domainInfo(overrides: Partial<DomainInfo> = {}): DomainInfo {
  return {
    hostname: "www.example.com",
    root_domain: "example.com",
    asn: 13335,
    asn_name: "CLOUDFLARENET",
    waf: true,
    updated_at: "2026-10-27T12:30:00Z",
    ...overrides,
  };
}

export function dnsRecord(overrides: Partial<DnsRecord> = {}): DnsRecord {
  return { hostname: "www.example.com", record_type: "A", record_value: "192.0.2.1", ttl: 300, updated_at: "2026-10-27T12:30:00Z", ...overrides };
}
