import { render } from "@testing-library/react";
import { createMemoryRouter, RouterProvider } from "react-router";
import { vi } from "vitest";

import { Providers } from "@/app/providers";
import { routeObjects } from "@/app/router";
import { setApiKey } from "@/lib/api-key";
import { createQueryClient } from "@/lib/query-client";

export const TEST_KEY = "test-key-0123456789abcdef0123456789";

type Handler = (url: URL, init: RequestInit) => Response | Promise<Response>;

/** Replaces fetch; returns the mock so tests can inspect calls. */
export function mockFetch(handler: Handler) {
  const fn = vi.fn(async (input: RequestInfo | URL, init: RequestInit = {}) =>
    handler(new URL(String(input), "http://localhost"), init),
  );
  vi.stubGlobal("fetch", fn);
  return fn;
}

export function json(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } });
}

/** Renders the real route tree at `path` with an authenticated session. */
export function renderRoute(path: string, { apiKey = TEST_KEY }: { apiKey?: string | null } = {}) {
  setApiKey(apiKey);
  const queryClient = createQueryClient();
  queryClient.setDefaultOptions({ queries: { ...queryClient.getDefaultOptions().queries, retry: false } });
  const router = createMemoryRouter(routeObjects, { initialEntries: [path] });
  const result = render(
    <Providers client={queryClient}>
      <RouterProvider router={router} />
    </Providers>,
  );
  return { ...result, router, queryClient };
}
