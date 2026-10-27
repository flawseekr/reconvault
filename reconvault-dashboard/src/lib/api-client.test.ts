import { describe, expect, it, vi } from "vitest";

import { ApiError, apiRequest, buildUrl } from "@/lib/api-client";
import { getApiKey, setApiKey } from "@/lib/api-key";
import { json, mockFetch, TEST_KEY } from "@/test/utils";

describe("buildUrl", () => {
  it("encodes query values and drops empty ones", () => {
    expect(buildUrl("/api/v1/http/artifacts", { limit: 50, hostname: "a b&c", cursor: undefined, q: "", x: null })).toBe(
      "/api/v1/http/artifacts?limit=50&hostname=a+b%26c",
    );
    expect(buildUrl("/readyz")).toBe("/readyz");
  });
});

describe("apiRequest", () => {
  it("sends the API key and parses JSON", async () => {
    setApiKey(TEST_KEY);
    const fetch = mockFetch(() => json({ data: [1] }));
    await expect(apiRequest("/api/v1/http/domains")).resolves.toEqual({ data: [1] });
    const init = fetch.mock.calls[0]?.[1];
    expect((init?.headers as Record<string, string>).Authorization).toBe(`Bearer ${TEST_KEY}`);
    expect(init?.credentials).toBe("omit");
  });

  it("does not call /api/v1 endpoints without a key, but allows public ones", async () => {
    setApiKey(null);
    const fetch = mockFetch(() => json({ status: "ok" }));
    await expect(apiRequest("/api/v1/http/artifacts")).rejects.toMatchObject({ status: 401, code: "unauthorized" });
    expect(fetch).not.toHaveBeenCalled();
    await expect(apiRequest("/readyz")).resolves.toEqual({ status: "ok" });
    expect(fetch).toHaveBeenCalledTimes(1);
  });

  it("serializes JSON bodies", async () => {
    setApiKey(TEST_KEY);
    const fetch = mockFetch(() => json({ id: "x" }, 201));
    await apiRequest("/api/v1/http/request", { method: "POST", body: { method: "GET", url: "https://a.com" } });
    const init = fetch.mock.calls[0]?.[1];
    expect(init?.method).toBe("POST");
    expect(init?.body).toBe('{"method":"GET","url":"https://a.com"}');
    expect((init?.headers as Record<string, string>)["Content-Type"]).toBe("application/json");
  });

  it("maps the API error envelope", async () => {
    setApiKey(TEST_KEY);
    mockFetch(() => json({ error: { code: "upstream_request_failed", message: "the upstream request failed", artifact_id: "abc" } }, 502));
    const err = await apiRequest("/api/v1/http/request").catch((e: unknown) => e);
    expect(err).toBeInstanceOf(ApiError);
    expect(err).toMatchObject({ status: 502, code: "upstream_request_failed", artifactId: "abc", message: "the upstream request failed" });
  });

  it("clears the stored key on 401", async () => {
    setApiKey(TEST_KEY);
    mockFetch(() => json({ error: { code: "unauthorized", message: "missing or invalid API key" } }, 401));
    await expect(apiRequest("/api/v1/http/artifacts")).rejects.toMatchObject({ status: 401 });
    expect(getApiKey()).toBeNull();
  });

  it("handles non-JSON errors, 204 and invalid success bodies", async () => {
    setApiKey(TEST_KEY);
    mockFetch(() => new Response("<html>bad gateway</html>", { status: 502 }));
    await expect(apiRequest("/x")).rejects.toMatchObject({ status: 502, code: "http_error" });
    mockFetch(() => new Response(null, { status: 204 }));
    await expect(apiRequest("/x", { method: "DELETE" })).resolves.toBeUndefined();
    mockFetch(() => new Response("not json", { status: 200 }));
    await expect(apiRequest("/x")).rejects.toMatchObject({ code: "invalid_response" });
  });

  it("reports network failures", async () => {
    mockFetch(() => {
      throw new TypeError("Failed to fetch");
    });
    await expect(apiRequest("/x")).rejects.toMatchObject({ status: 0, code: "network_error" });
  });

  it("times out", async () => {
    vi.stubGlobal("fetch", (_: unknown, init: RequestInit) =>
      new Promise((_resolve, reject) => init.signal?.addEventListener("abort", () => reject(new DOMException("aborted", "AbortError")))),
    );
    await expect(apiRequest("/x", { timeoutMs: 20 })).rejects.toMatchObject({ code: "timeout" });
  });

  it("propagates caller cancellation as an abort, not an ApiError", async () => {
    vi.stubGlobal("fetch", (_: unknown, init: RequestInit) =>
      new Promise((_resolve, reject) => init.signal?.addEventListener("abort", () => reject(new DOMException("aborted", "AbortError")))),
    );
    const controller = new AbortController();
    const pending = apiRequest("/x", { signal: controller.signal });
    controller.abort();
    const err = await pending.catch((e: unknown) => e);
    expect(err).not.toBeInstanceOf(ApiError);
    expect((err as DOMException).name).toBe("AbortError");
  });
});

describe("setApiKey", () => {
  it("notifies subscribers only when the key changes", async () => {
    const { subscribeApiKey } = await import("@/lib/api-key");
    setApiKey(null);
    const listener = vi.fn();
    const unsubscribe = subscribeApiKey(listener);
    setApiKey(null);
    setApiKey("   ");
    expect(listener).not.toHaveBeenCalled();
    setApiKey(TEST_KEY);
    setApiKey(` ${TEST_KEY} `);
    expect(listener).toHaveBeenCalledTimes(1);
    setApiKey(null);
    expect(listener).toHaveBeenCalledTimes(2);
    unsubscribe();
  });
});
