export interface HeaderEntry {
  name: string;
  value: string;
}

export interface ParsedResponse {
  /** e.g. "HTTP/1.1 200 OK"; null for legacy body-only records. */
  statusLine: string | null;
  /** null when the record holds no headers (pre-0004 artifacts). */
  headers: HeaderEntry[] | null;
  body: string;
}

const STATUS_LINE = /^HTTP\/\d(?:\.\d)? \d{3}\b/;

/**
 * Splits the API's raw `response` (status line, headers, CRLF CRLF, body)
 * into parts. Artifacts recorded before the raw-response migration contain
 * only the body; those are returned with statusLine and headers null.
 */
export function parseRawResponse(raw: string): ParsedResponse {
  const end = raw.indexOf("\r\n\r\n");
  if (!STATUS_LINE.test(raw) || end === -1) {
    return { statusLine: null, headers: null, body: raw };
  }
  const [statusLine = "", ...lines] = raw.slice(0, end).split("\r\n");
  const headers = lines
    .filter((line) => line.length > 0)
    .map((line) => {
      const i = line.indexOf(":");
      return i === -1
        ? { name: line, value: "" }
        : { name: line.slice(0, i), value: line.slice(i + 1).trim() };
    });
  return { statusLine, headers, body: raw.slice(end + 4) };
}

export function headersFromRecord(record: Record<string, string> | null): HeaderEntry[] | null {
  if (!record) return null;
  return Object.entries(record)
    .map(([name, value]) => ({ name, value }))
    .sort((a, b) => a.name.localeCompare(b.name));
}

export function headerValue(headers: HeaderEntry[] | null, name: string): string | null {
  const lower = name.toLowerCase();
  return headers?.find((h) => h.name.toLowerCase() === lower)?.value ?? null;
}

export function formatHeaders(headers: HeaderEntry[]): string {
  return headers.map((h) => `${h.name}: ${h.value}`).join("\n");
}
