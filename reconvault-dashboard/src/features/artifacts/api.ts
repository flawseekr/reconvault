import { keepPreviousData, useMutation, useQuery, useQueryClient } from "@tanstack/react-query";

import { apiRequest } from "@/lib/api-client";
import type { Artifact, ArtifactList, ArtifactSortField, ExecuteRequest, ExecuteResponse, SortOrder } from "@/types/api";

export interface ArtifactListParams {
  sortBy: ArtifactSortField;
  sortOrder: SortOrder;
  limit: number;
  hostname?: string;
  cursor?: string;
}

export const artifactKeys = {
  all: ["artifacts"] as const,
  lists: () => [...artifactKeys.all, "list"] as const,
  list: (p: ArtifactListParams) => [...artifactKeys.lists(), p] as const,
  detail: (id: string) => [...artifactKeys.all, "detail", id] as const,
};

/** GET /api/v1/http/artifacts with server-side sorting and cursor pagination. */
export function useArtifactList(params: ArtifactListParams) {
  return useQuery({
    queryKey: artifactKeys.list(params),
    queryFn: ({ signal }) =>
      apiRequest<ArtifactList>("/api/v1/http/artifacts", {
        signal,
        query: {
          sort_by: params.sortBy,
          sort_order: params.sortOrder,
          limit: params.limit,
          hostname: params.hostname,
          cursor: params.cursor,
        },
      }),
    // Keep the current rows visible while a new sort/page loads.
    placeholderData: keepPreviousData,
  });
}

/** GET /api/v1/http/artifacts/{id}. Artifacts are immutable once recorded. */
export function useArtifact(id: string | undefined) {
  return useQuery({
    queryKey: artifactKeys.detail(id ?? ""),
    queryFn: ({ signal }) => apiRequest<Artifact>(`/api/v1/http/artifacts/${encodeURIComponent(id ?? "")}`, { signal }),
    enabled: id !== undefined,
    staleTime: Infinity,
  });
}

/** Outbound requests can legitimately take up to the API's upstream timeout. */
const EXECUTE_TIMEOUT_MS = 90_000;

/** POST /api/v1/http/request. Refreshes listings, hosts and search afterwards. */
export function useExecuteRequest() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (input: ExecuteRequest) =>
      apiRequest<ExecuteResponse>("/api/v1/http/request", { method: "POST", body: input, timeoutMs: EXECUTE_TIMEOUT_MS }),
    onSettled: () =>
      // A failed upstream attempt is recorded as well, so refresh either way.
      Promise.all([
        queryClient.invalidateQueries({ queryKey: artifactKeys.lists() }),
        queryClient.invalidateQueries({ queryKey: ["domains"] }),
        queryClient.invalidateQueries({ queryKey: ["search"] }),
      ]),
  });
}
