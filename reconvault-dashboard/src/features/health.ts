import { useQuery } from "@tanstack/react-query";

import { apiRequest } from "@/lib/api-client";

/** GET /readyz: API and database reachability for the status indicator. */
export function useApiHealth() {
  return useQuery({
    queryKey: ["health"],
    queryFn: ({ signal }) => apiRequest<{ status: string }>("/readyz", { signal, timeoutMs: 5_000 }),
    refetchInterval: 30_000,
    retry: false,
    staleTime: 25_000,
  });
}
