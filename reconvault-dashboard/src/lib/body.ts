export type BodyKind = "empty" | "json" | "html" | "xml" | "text" | "binary";

export interface InspectedBody {
  kind: BodyKind;
  /** Pretty-printed JSON when kind is "json"; the original text otherwise. */
  display: string;
}

const REPLACEMENT = "\uFFFD";
const BINARY_TYPES = /^(image|audio|video|font)\/|application\/(octet-stream|pdf|zip|gzip|x-tar|wasm|x-protobuf|protobuf)/i;

/**
 * Classifies a captured body for display. The API stores bodies as text
 * with invalid UTF-8 replaced by U+FFFD, so a high share of replacement
 * characters (or a binary media type) marks binary content.
 */
export function inspectBody(body: string | null | undefined, contentType: string | null | undefined): InspectedBody {
  if (!body) return { kind: "empty", display: "" };
  const type = (contentType ?? "").toLowerCase();

  if (BINARY_TYPES.test(type) || replacementRatio(body) > 0.1) {
    return { kind: "binary", display: body };
  }
  if (type.includes("json") || /^\s*[[{]/.test(body)) {
    try {
      return { kind: "json", display: JSON.stringify(JSON.parse(body), null, 2) };
    } catch {
      if (type.includes("json")) return { kind: "text", display: body };
    }
  }
  if (type.includes("html")) return { kind: "html", display: body };
  if (type.includes("xml")) return { kind: "xml", display: body };
  return { kind: "text", display: body };
}

function replacementRatio(s: string): number {
  const sample = s.slice(0, 4096);
  let n = 0;
  for (const ch of sample) if (ch === REPLACEMENT) n++;
  return n / sample.length;
}

export type JsonTokenType = "key" | "string" | "number" | "literal" | "punct" | "space";
export interface JsonToken {
  type: JsonTokenType;
  text: string;
}

const JSON_TOKEN = /("(?:\\.|[^"\\])*")(\s*:)?|(-?\d+(?:\.\d+)?(?:[eE][+-]?\d+)?)|(true|false|null)|([{}[\],:])|(\s+)/g;

/**
 * Tokenizes pretty-printed JSON for syntax highlighting. Tokens are
 * rendered as React text nodes, never as HTML, so captured content cannot
 * inject markup.
 */
export function tokenizeJson(json: string): JsonToken[] {
  const tokens: JsonToken[] = [];
  let last = 0;
  for (const m of json.matchAll(JSON_TOKEN)) {
    const index = m.index ?? 0;
    if (index > last) tokens.push({ type: "punct", text: json.slice(last, index) });
    const [whole, str, colon, num, literal, punct] = m;
    if (str !== undefined) {
      tokens.push({ type: colon ? "key" : "string", text: str });
      if (colon) tokens.push({ type: "punct", text: colon });
    } else if (num !== undefined) tokens.push({ type: "number", text: num });
    else if (literal !== undefined) tokens.push({ type: "literal", text: literal });
    else if (punct !== undefined) tokens.push({ type: "punct", text: punct });
    else tokens.push({ type: "space", text: whole });
    last = index + whole.length;
  }
  if (last < json.length) tokens.push({ type: "punct", text: json.slice(last) });
  return tokens;
}
