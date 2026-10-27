import { describe, expect, it } from "vitest";

import { hasControlChars, isUuid, isValidHostnameParam, targetUrlSchema, validateSearchQuery } from "@/lib/validation";

describe("validateSearchQuery", () => {
  it("accepts normal queries including symbols and surrounding spaces", () => {
    for (const q of ["secret", " Bearer ", "100%", "a_b\\c", "日本語"]) expect(validateSearchQuery(q)).toBeNull();
    expect(validateSearchQuery("a".repeat(1024))).toBeNull();
  });
  it("rejects blank, oversized and control-character queries", () => {
    expect(validateSearchQuery("")).not.toBeNull();
    expect(validateSearchQuery("   ")).not.toBeNull();
    expect(validateSearchQuery("a".repeat(1025))).not.toBeNull();
    expect(validateSearchQuery("é".repeat(513))).not.toBeNull(); // 1026 bytes
    expect(validateSearchQuery("a\u001fb")).not.toBeNull();
    expect(validateSearchQuery("a\u007f")).not.toBeNull();
  });
});

describe("hasControlChars", () => {
  it("detects C0 and DEL only", () => {
    expect(hasControlChars("plain text")).toBe(false);
    expect(hasControlChars("tab\there")).toBe(true);
    expect(hasControlChars("\u0000")).toBe(true);
    expect(hasControlChars("\u007f")).toBe(true);
    expect(hasControlChars("\u0080")).toBe(false);
  });
});

describe("isUuid", () => {
  it("accepts canonical UUIDs in any case", () => {
    expect(isUuid("550e8400-e29b-41d4-a716-446655440000")).toBe(true);
    expect(isUuid("550E8400-E29B-41D4-A716-446655440000")).toBe(true);
  });
  it("rejects other forms", () => {
    for (const v of [undefined, "", "550e8400e29b41d4a716446655440000", "{550e8400-e29b-41d4-a716-446655440000}", "550e8400-e29b-41d4-a716-44665544000g"]) {
      expect(isUuid(v)).toBe(false);
    }
  });
});

describe("isValidHostnameParam", () => {
  it("validates like the API", () => {
    expect(isValidHostnameParam("api.example.com")).toBe(true);
    expect(isValidHostnameParam("h".repeat(255))).toBe(true);
    expect(isValidHostnameParam("h".repeat(256))).toBe(false);
    expect(isValidHostnameParam("  ")).toBe(false);
    expect(isValidHostnameParam(undefined)).toBe(false);
  });
});

describe("targetUrlSchema", () => {
  it("accepts absolute http(s) URLs", () => {
    expect(targetUrlSchema.safeParse("https://example.com").success).toBe(true);
    expect(targetUrlSchema.safeParse("  http://a.com:8080/x?y=1  ").data).toBe("http://a.com:8080/x?y=1");
  });
  it("rejects other input", () => {
    for (const v of ["", "example.com", "ftp://a.com", "javascript:alert(1)", "/relative", "http://"]) {
      expect(targetUrlSchema.safeParse(v).success).toBe(false);
    }
  });
});
