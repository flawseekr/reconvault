const BYTE_UNITS = ["B", "KB", "MB", "GB", "TB"] as const;

/** 1536 -> "1.5 KB" (binary multiples, as in most developer tools). */
export function formatBytes(bytes: number): string {
  if (!Number.isFinite(bytes) || bytes < 0) return "—";
  if (bytes < 1024) return `${bytes} B`;
  let value = bytes;
  let unit = 0;
  while (value >= 1024 && unit < BYTE_UNITS.length - 1) {
    value /= 1024;
    unit++;
  }
  return `${value >= 100 ? Math.round(value) : value.toFixed(1).replace(/\.0$/, "")} ${BYTE_UNITS[unit]}`;
}

export function formatInteger(n: number): string {
  return new Intl.NumberFormat("en-US").format(n);
}

function parse(ts: string): Date | null {
  const d = new Date(ts);
  return Number.isNaN(d.getTime()) ? null : d;
}

const dateTimeFormat = new Intl.DateTimeFormat(undefined, {
  year: "numeric",
  month: "short",
  day: "2-digit",
  hour: "2-digit",
  minute: "2-digit",
  second: "2-digit",
  hour12: false,
});

/** Compact local date-time, e.g. "Oct 27, 2026, 14:01:42". */
export function formatDateTime(ts: string): string {
  const d = parse(ts);
  return d ? dateTimeFormat.format(d) : "—";
}

/** Exact UTC instant with sub-second precision as stored by the API. */
export function formatExactUtc(ts: string): string {
  const d = parse(ts);
  if (!d) return ts;
  // Keep the API's microseconds rather than Date's millisecond rounding.
  const fraction = /\.(\d+)/.exec(ts)?.[1];
  const base = d.toISOString().slice(0, 19).replace("T", " ");
  return `${base}${fraction ? `.${fraction}` : ""} UTC`;
}

const relative = new Intl.RelativeTimeFormat("en", { numeric: "auto" });
const RELATIVE_STEPS: [Intl.RelativeTimeFormatUnit, number][] = [
  ["second", 60],
  ["minute", 60],
  ["hour", 24],
  ["day", 30],
  ["month", 12],
  ["year", Infinity],
];

/** "3 minutes ago"; `now` is injectable for tests. */
export function formatRelative(ts: string, now: number = Date.now()): string {
  const d = parse(ts);
  if (!d) return "—";
  let diff = (d.getTime() - now) / 1000;
  for (const [unit, size] of RELATIVE_STEPS) {
    if (Math.abs(diff) < size) return relative.format(Math.round(diff), unit);
    diff /= size;
  }
  return formatDateTime(ts);
}

export type StatusClass = "2xx" | "3xx" | "4xx" | "5xx" | "1xx" | "none";

export function statusClass(code: number | null | undefined): StatusClass {
  if (code === null || code === undefined) return "none";
  if (code >= 500) return "5xx";
  if (code >= 400) return "4xx";
  if (code >= 300) return "3xx";
  if (code >= 200) return "2xx";
  return "1xx";
}

/** Shortens a URL for display while keeping host and path tail readable. */
/**
 * Path and query of a URL ("https://a.com/x?y=1" → "/x?y=1"), without the
 * scheme, host or fragment. A URL with no path yields "/"; unparseable input
 * yields `fallbackPath`, or "/".
 */
export function urlPathAndQuery(url: string, fallbackPath?: string): string {
  try {
    const { pathname, search } = new URL(url);
    return `${pathname || "/"}${search}`;
  } catch {
    return fallbackPath || "/";
  }
}

export function middleTruncate(text: string, max: number): string {
  if (text.length <= max) return text;
  const keepEnd = Math.floor((max - 1) * 0.4);
  const keepStart = max - 1 - keepEnd;
  return `${text.slice(0, keepStart)}…${text.slice(text.length - keepEnd)}`;
}
