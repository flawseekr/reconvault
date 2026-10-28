import { describe, expect, it } from "vitest";

import { formatBytes, formatExactUtc, formatRelative, middleTruncate, statusClass, urlPathAndQuery } from "@/lib/formatters";

describe("formatBytes", () => {
  it.each([
    [0, "0 B"],
    [1023, "1023 B"],
    [1024, "1 KB"],
    [1536, "1.5 KB"],
    [10 * 1024 * 1024, "10 MB"],
    [150 * 1024, "150 KB"],
    [5 * 1024 ** 4, "5 TB"],
  ])("%i -> %s", (input, want) => expect(formatBytes(input)).toBe(want));

  it("rejects invalid input", () => {
    expect(formatBytes(-1)).toBe("—");
    expect(formatBytes(Number.NaN)).toBe("—");
  });
});

describe("formatExactUtc", () => {
  it("keeps microsecond precision from the API", () => {
    expect(formatExactUtc("2026-10-27T14:01:42.629027Z")).toBe("2026-10-27 14:01:42.629027 UTC");
  });
  it("handles whole seconds and invalid input", () => {
    expect(formatExactUtc("2026-10-27T14:01:42Z")).toBe("2026-10-27 14:01:42 UTC");
    expect(formatExactUtc("nope")).toBe("nope");
  });
});

describe("formatRelative", () => {
  const now = Date.parse("2026-10-27T12:00:00Z");
  it.each([
    ["2026-10-27T11:59:30Z", "30 seconds ago"],
    ["2026-10-27T11:55:00Z", "5 minutes ago"],
    ["2026-10-27T09:00:00Z", "3 hours ago"],
    ["2026-10-26T12:00:00Z", "yesterday"],
  ])("%s -> %s", (ts, want) => expect(formatRelative(ts, now)).toBe(want));
});

describe("statusClass", () => {
  it.each([
    [null, "none"],
    [101, "1xx"],
    [200, "2xx"],
    [299, "2xx"],
    [301, "3xx"],
    [404, "4xx"],
    [500, "5xx"],
    [599, "5xx"],
  ] as const)("%s -> %s", (code, want) => expect(statusClass(code)).toBe(want));
});

describe("urlPathAndQuery", () => {
  it("drops scheme, host and fragment", () => {
    expect(urlPathAndQuery("https://example.com/api/v1/users")).toBe("/api/v1/users");
    expect(urlPathAndQuery("http://example.com:8080/a/b#top")).toBe("/a/b");
  });
  it("keeps the query string", () => {
    expect(urlPathAndQuery("https://example.com/promos/?type=Semua")).toBe("/promos/?type=Semua");
    expect(urlPathAndQuery("https://example.com?x=1")).toBe("/?x=1");
  });
  it("returns / for root and path-less URLs", () => {
    expect(urlPathAndQuery("https://example.com/")).toBe("/");
    expect(urlPathAndQuery("https://example.com")).toBe("/");
  });
  it("falls back to the stored path for unparseable input", () => {
    expect(urlPathAndQuery("not a url", "/stored")).toBe("/stored");
    expect(urlPathAndQuery("not a url")).toBe("/");
  });
});

describe("middleTruncate", () => {
  it("leaves short text alone and keeps both ends of long text", () => {
    expect(middleTruncate("short", 10)).toBe("short");
    const out = middleTruncate("https://example.com/a/very/long/path/to/resource.json", 40);
    expect(out).toHaveLength(40);
    expect(out.startsWith("https://example.com")).toBe(true);
    expect(out.endsWith("resource.json")).toBe(true);
    expect(out).toContain("…");
  });
});
