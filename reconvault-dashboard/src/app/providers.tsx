import { type QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { type ReactNode, useEffect, useState } from "react";

import { TooltipProvider } from "@/components/ui/tooltip";
import { subscribeApiKey } from "@/lib/api-key";
import { createQueryClient } from "@/lib/query-client";

interface ProvidersProps {
  children: ReactNode;
  /** Injectable for tests; created once per app otherwise. */
  client?: QueryClient;
}

export function Providers({ children, client }: ProvidersProps) {
  const [queryClient] = useState(() => client ?? createQueryClient());

  // A new key may grant access that the previous one lacked: refetch.
  useEffect(() => subscribeApiKey(() => void queryClient.invalidateQueries()), [queryClient]);

  return (
    <QueryClientProvider client={queryClient}>
      <TooltipProvider>{children}</TooltipProvider>
    </QueryClientProvider>
  );
}
