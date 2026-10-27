import { describe, expect, it } from "vitest";

import { inspectBody, tokenizeJson } from "@/lib/body";

describe("inspectBody", () => {
  it("pretty-prints JSON by content type or shape", () => {
    expect(inspectBody('{"a":1}', "application/json; charset=utf-8")).toEqual({ kind: "json", display: '{\n  "a": 1\n}' });
    expect(inspectBody("[1,2]", "text/plain").kind).toBe("json");
    expect(inspectBody('{"a":1}', "application/problem+json").kind).toBe("json");
  });

  it("falls back to text for invalid JSON", () => {
    expect(inspectBody("{not json", "application/json")).toEqual({ kind: "text", display: "{not json" });
  });

  it("classifies html, xml, text, empty and binary", () => {
    expect(inspectBody("<p>x</p>", "text/html").kind).toBe("html");
    expect(inspectBody("<a/>", "application/xml").kind).toBe("xml");
    expect(inspectBody("hello", null).kind).toBe("text");
    expect(inspectBody("", "text/plain").kind).toBe("empty");
    expect(inspectBody(null, null).kind).toBe("empty");
    expect(inspectBody("PNG", "image/png").kind).toBe("binary");
    expect(inspectBody("���ab", null).kind).toBe("binary");
    expect(inspectBody("mostly text � here and more text", null).kind).toBe("text");
  });
});

describe("tokenizeJson", () => {
  const sample = JSON.stringify({ key: "va\"lue", n: -1.5e3, ok: true, none: null, list: [1, "<script>"] }, null, 2);

  it("round-trips the input exactly", () => {
    expect(tokenizeJson(sample).map((t) => t.text).join("")).toBe(sample);
  });

  it("classifies keys, strings, numbers and literals", () => {
    const tokens = tokenizeJson(sample);
    const of = (type: string) => tokens.filter((t) => t.type === type).map((t) => t.text);
    expect(of("key")).toEqual(['"key"', '"n"', '"ok"', '"none"', '"list"']);
    expect(of("string")).toEqual(['"va\\"lue"', '"<script>"']);
    expect(of("number")).toEqual(["-1500", "1"]);
    expect(of("literal")).toEqual(["true", "null"]);
  });
});
