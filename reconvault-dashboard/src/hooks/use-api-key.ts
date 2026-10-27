import { useSyncExternalStore } from "react";

import { getApiKey, subscribeApiKey } from "@/lib/api-key";

export function useApiKey(): string | null {
  return useSyncExternalStore(subscribeApiKey, getApiKey, getApiKey);
}
