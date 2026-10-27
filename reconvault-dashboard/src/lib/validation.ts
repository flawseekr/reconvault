import { z } from "zod";

import { LIMITS } from "@/types/api";

const UUID = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i;

/** True when s contains C0 control characters or DEL (the API rejects them). */
export function hasControlChars(s: string): boolean {
  for (let i = 0; i < s.length; i++) {
    const c = s.charCodeAt(i);
    if (c < 0x20 || c === 0x7f) return true;
  }
  return false;
}

export function isUuid(value: string | undefined): value is string {
  return value !== undefined && UUID.test(value);
}

/** Mirrors the API's rules for GET /api/v1/http/artifacts/search `q`. */
export function validateSearchQuery(q: string): string | null {
  if (q.trim() === "") return "Enter a search term.";
  if (new TextEncoder().encode(q).length > LIMITS.queryMaxBytes) {
    return `Search terms are limited to ${LIMITS.queryMaxBytes} bytes.`;
  }
  if (hasControlChars(q)) return "Search terms cannot contain control characters.";
  return null;
}

/** Mirrors the API's hostname filter rules. */
export function isValidHostnameParam(hostname: string | undefined): hostname is string {
  return (
    hostname !== undefined &&
    hostname.trim() !== "" &&
    hostname.length <= LIMITS.hostnameMaxLength &&
    !hasControlChars(hostname)
  );
}

/** URL submitted from the dashboard; the API re-validates server-side. */
export const targetUrlSchema = z
  .string()
  .trim()
  .min(1, "Enter a URL.")
  .max(LIMITS.urlMaxLength, `URLs are limited to ${LIMITS.urlMaxLength} characters.`)
  .refine((value) => {
    try {
      const url = new URL(value);
      return (url.protocol === "http:" || url.protocol === "https:") && url.hostname !== "";
    } catch {
      return false;
    }
  }, "Enter an absolute http:// or https:// URL.");
