import { describe, expect, it } from "vitest";

import { formatHeaders, headersFromRecord, headerValue, parseRawResponse } from "@/lib/http-message";

describe("parseRawResponse", () => {
  it("splits status line, headers and body", () => {
    const raw = 'HTTP/1.1 200 OK\r\nContent-Type: application/json\r\nSet-Cookie: a=1\r\nSet-Cookie: b=2\r\nX-Empty:\r\n\r\n{"ok":true}\r\n\r\ntrailing';
    const parsed = parseRawResponse(raw);
    expect(parsed.statusLine).toBe("HTTP/1.1 200 OK");
    expect(parsed.headers).toEqual([
      { name: "Content-Type", value: "application/json" },
      { name: "Set-Cookie", value: "a=1" },
      { name: "Set-Cookie", value: "b=2" },
      { name: "X-Empty", value: "" },
    ]);
    // Only the first blank line separates head and body.
    expect(parsed.body).toBe('{"ok":true}\r\n\r\ntrailing');
  });

  it("supports HTTP/2 status lines and values containing colons", () => {
    const parsed = parseRawResponse("HTTP/2.0 404 Not Found\r\nLocation: https://a.com:8443/x\r\n\r\n");
    expect(parsed.statusLine).toBe("HTTP/2.0 404 Not Found");
    expect(parsed.headers).toEqual([{ name: "Location", value: "https://a.com:8443/x" }]);
    expect(parsed.body).toBe("");
  });

  it("treats legacy body-only records as body", () => {
    for (const raw of ['{"legacy":true}', "HTTP/1.1 200 OK but no blank line", "<html>HTTP/1.1 200 OK\r\n\r\n</html>"]) {
      const parsed = parseRawResponse(raw);
      expect(parsed).toEqual({ statusLine: null, headers: null, body: raw });
    }
  });
});

describe("header helpers", () => {
  it("sorts record headers and looks up case-insensitively", () => {
    const h = headersFromRecord({ "User-Agent": "ua", "Content-Type": "application/json" });
    expect(h?.map((x) => x.name)).toEqual(["Content-Type", "User-Agent"]);
    expect(headerValue(h, "content-type")).toBe("application/json");
    expect(headerValue(h, "missing")).toBeNull();
    expect(headersFromRecord(null)).toBeNull();
    expect(formatHeaders(h ?? [])).toBe("Content-Type: application/json\nUser-Agent: ua");
  });
});
