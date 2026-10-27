import { useLocation, useNavigate, useSearchParams } from "react-router";

interface PaginationState {
  /** Cursors of the pages before the current one ("" = first page). */
  previous: string[];
}

function readState(state: unknown): PaginationState | null {
  if (typeof state !== "object" || state === null || !("previous" in state)) return null;
  const previous = (state as { previous: unknown }).previous;
  return Array.isArray(previous) && previous.every((c) => typeof c === "string") ? { previous } : null;
}

/**
 * Keyset pagination over the API's opaque cursors. The current cursor lives
 * in the URL (`cursor`) and the trail of earlier cursors in the history
 * entry's state, so Previous works and survives refreshes. A shared link
 * carries only the cursor: its page number is unknown and Previous returns
 * to the first page.
 */
export function useCursorPagination() {
  const [params] = useSearchParams();
  const location = useLocation();
  const navigate = useNavigate();

  const cursor = params.get("cursor") ?? undefined;
  const state = readState(location.state);
  const previous = state?.previous ?? [];
  const pageKnown = cursor === undefined || state !== null;
  const page = cursor === undefined ? 1 : pageKnown ? previous.length + 1 : null;

  function go(nextCursor: string | undefined, nextPrevious: string[]) {
    const p = new URLSearchParams(params);
    if (nextCursor) p.set("cursor", nextCursor);
    else p.delete("cursor");
    const search = p.toString();
    navigate({ search: search ? `?${search}` : "" }, { state: { previous: nextPrevious } satisfies PaginationState });
  }

  const goNext = (nextCursor: string) => go(nextCursor, [...previous, cursor ?? ""]);

  function goPrevious() {
    if (!pageKnown) return go(undefined, []);
    go(previous.at(-1) || undefined, previous.slice(0, -1));
  }

  const goFirst = () => go(undefined, []);

  return { cursor, page, hasPrevious: cursor !== undefined, goNext, goPrevious, goFirst };
}
