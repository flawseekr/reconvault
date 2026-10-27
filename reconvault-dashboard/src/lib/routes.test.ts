import { describe, expect, it } from "vitest";

import { hostPath, subdomainPath } from "@/lib/routes";

describe("route builders", () => {
  it.each([
    ["example.com", "/subdomain/example.com"],
    ["a/b?c#d", "/subdomain/a%2Fb%3Fc%23d"],
    ["../x", "/subdomain/..%2Fx"],
  ])("subdomainPath(%j) -> %s", (input, want) => {
    expect(subdomainPath(input)).toBe(want);
  });

  it("encodes hostnames", () => {
    expect(hostPath("a b/c")).toBe("/host/a%20b%2Fc");
  });
});
