import { useQuery } from "@tanstack/react-query";

import { apiRequest } from "@/lib/api-client";
import type { DomainList } from "@/types/api";

export const domainKeys = { all: ["domains"] as const };

/** GET /api/v1/http/domains (server default: latest_update DESC, limit 100). */
export function useDomains() {
  return useQuery({
    queryKey: domainKeys.all,
    queryFn: ({ signal }) => apiRequest<DomainList>("/api/v1/http/domains", { signal }),
    staleTime: 30_000,
  });
}
