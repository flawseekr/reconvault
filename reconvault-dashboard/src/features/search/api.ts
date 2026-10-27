import { keepPreviousData, useQuery } from "@tanstack/react-query";

import { apiRequest } from "@/lib/api-client";
import type { SearchResponse } from "@/types/api";

export const searchKeys = {
  all: ["search"] as const,
  query: (q: string, limit: number) => [...searchKeys.all, q, limit] as const,
};

/**
 * GET /api/v1/http/artifacts/search. Each (q, limit) pair is its own cache
 * entry and superseded requests are aborted, so a slow earlier response
 * can never overwrite newer results.
 */
export function useArtifactSearch(q: string | null, limit: number) {
  return useQuery({
    queryKey: searchKeys.query(q ?? "", limit),
    queryFn: ({ signal }) => apiRequest<SearchResponse>("/api/v1/http/artifacts/search", { signal, query: { q, limit } }),
    enabled: q !== null,
    placeholderData: keepPreviousData,
    staleTime: 30_000,
  });
}
