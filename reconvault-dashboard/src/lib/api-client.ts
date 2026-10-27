import { getApiKey, setApiKey } from "@/lib/api-key";
import type { ApiErrorBody } from "@/types/api";

export const DEFAULT_TIMEOUT_MS = 15_000;

export class ApiError extends Error {
  readonly status: number;
  readonly code: string;
  readonly field?: string;
  readonly artifactId?: string;

  constructor(status: number, code: string, message: string, extra?: { field?: string; artifactId?: string }) {
    super(message);
    this.name = "ApiError";
    this.status = status;
    this.code = code;
    this.field = extra?.field;
    this.artifactId = extra?.artifactId;
  }

  get isNotFound() {
    return this.status === 404;
  }
  get isUnauthorized() {
    return this.status === 401;
  }
}

type QueryValue = string | number | undefined | null;

export interface RequestOptions {
  method?: "GET" | "POST" | "DELETE";
  query?: Record<string, QueryValue>;
  body?: unknown;
  signal?: AbortSignal;
  timeoutMs?: number;
}

export function buildUrl(path: string, query?: Record<string, QueryValue>): string {
  const params = new URLSearchParams();
  for (const [key, value] of Object.entries(query ?? {})) {
    if (value !== undefined && value !== null && value !== "") params.set(key, String(value));
  }
  const qs = params.toString();
  // Paths are same-origin: nginx (and the Vite dev server) proxy them to the API.
  return `${path}${qs ? `?${qs}` : ""}`;
}

function isErrorBody(value: unknown): value is ApiErrorBody {
  if (typeof value !== "object" || value === null || !("error" in value)) return false;
  const err = (value as { error: unknown }).error;
  return typeof err === "object" && err !== null && "code" in err && "message" in err;
}

/**
 * Performs a request against the ReconVault API. Rejects with ApiError for
 * HTTP errors (mapping the API's error envelope), network failures
 * (code "network_error") and timeouts (code "timeout"). Aborts requested by
 * the caller's signal propagate as the original AbortError so TanStack
 * Query can treat them as cancellations.
 */
export async function apiRequest<T>(path: string, options: RequestOptions = {}): Promise<T> {
  const { method = "GET", query, body, signal, timeoutMs = DEFAULT_TIMEOUT_MS } = options;
  const timeout = AbortSignal.timeout(timeoutMs);
  const combined = signal ? AbortSignal.any([signal, timeout]) : timeout;

  const key = getApiKey();
  // Every /api/v1 endpoint requires the key; don't send requests bound to fail.
  if (!key && path.startsWith("/api/v1/")) {
    throw new ApiError(401, "unauthorized", "An API key is required.");
  }

  const headers: Record<string, string> = { Accept: "application/json" };
  if (key) headers.Authorization = `Bearer ${key}`;
  if (body !== undefined) headers["Content-Type"] = "application/json";

  let response: Response;
  try {
    response = await fetch(buildUrl(path, query), {
      method,
      headers,
      body: body === undefined ? undefined : JSON.stringify(body),
      signal: combined,
      credentials: "omit",
      cache: "no-store",
    });
  } catch (err) {
    if (signal?.aborted) throw err;
    if (timeout.aborted) {
      throw new ApiError(0, "timeout", `The request timed out after ${Math.round(timeoutMs / 1000)}s.`);
    }
    throw new ApiError(0, "network_error", "Could not reach the ReconVault API.");
  }

  if (response.status === 204) return undefined as T;

  const text = await response.text();
  let payload: unknown = undefined;
  if (text) {
    try {
      payload = JSON.parse(text);
    } catch {
      payload = undefined;
    }
  }

  if (!response.ok) {
    if (response.status === 401) setApiKey(null); // prompt for a valid key
    if (isErrorBody(payload)) {
      const { code, message, field, artifact_id } = payload.error;
      throw new ApiError(response.status, code, message, { field, artifactId: artifact_id });
    }
    throw new ApiError(response.status, "http_error", `The API responded with HTTP ${response.status}.`);
  }
  if (payload === undefined) {
    throw new ApiError(response.status, "invalid_response", "The API returned an invalid response.");
  }
  return payload as T;
}

/** User-facing text for any error thrown by apiRequest or a query. */
export function errorMessage(error: unknown): string {
  if (error instanceof ApiError) return error.message;
  if (error instanceof Error) return error.message;
  return "Unexpected error.";
}
