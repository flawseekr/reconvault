import { useQuery } from "@tanstack/react-query";

import { apiRequest } from "@/lib/api-client";
import type { DnsRecordList, DomainInfo, RootDomainList, SubdomainList } from "@/types/api";

// Data from reconvault-dns-autoscan changes on a scan cadence, not per request.
const STALE_TIME = 60_000;

export const reconKeys = {
  domain: (hostname: string) => ["recon", "domain", hostname] as const,
  dnsHistory: (hostname: string) => ["recon", "dns", hostname] as const,
  subdomains: (rootDomain: string) => ["recon", "subdomains", rootDomain] as const,
  rootDomains: ["recon", "root-domains"] as const,
};

/** GET /api/v1/domains/{hostname}; rejects with a 404 ApiError when not scanned yet. */
export function useDomainInfo(hostname: string) {
  return useQuery({
    queryKey: reconKeys.domain(hostname),
    queryFn: ({ signal }) => apiRequest<DomainInfo>(`/api/v1/domains/${encodeURIComponent(hostname)}`, { signal }),
    staleTime: STALE_TIME,
  });
}

/** GET /api/v1/dns_records/{hostname}: the full history, newest first. */
export function useDnsHistory(hostname: string) {
  return useQuery({
    queryKey: reconKeys.dnsHistory(hostname),
    queryFn: ({ signal }) => apiRequest<DnsRecordList>(`/api/v1/dns_records/${encodeURIComponent(hostname)}`, { signal }),
    staleTime: STALE_TIME,
  });
}

/** GET /api/v1/subdomains/{root_domain}. */
export function useSubdomains(rootDomain: string) {
  return useQuery({
    queryKey: reconKeys.subdomains(rootDomain),
    queryFn: ({ signal }) => apiRequest<SubdomainList>(`/api/v1/subdomains/${encodeURIComponent(rootDomain)}`, { signal }),
    staleTime: STALE_TIME,
  });
}

/** GET /api/v1/root_domains (server default: hostname_count DESC, limit 100). */
export function useRootDomains() {
  return useQuery({
    queryKey: reconKeys.rootDomains,
    queryFn: ({ signal }) => apiRequest<RootDomainList>("/api/v1/root_domains", { signal }),
    staleTime: STALE_TIME,
  });
}
