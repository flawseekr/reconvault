import { useSearchParams } from "react-router";

import type { SortOrder } from "@/types/api";

export interface SortState<F extends string> {
  sortBy: F;
  sortOrder: SortOrder;
}

/**
 * Sort state stored in the URL (`sort_by`, `sort_order`) so views can be
 * bookmarked and shared. Unknown values fall back to the defaults. Changing
 * the sort drops `cursor`, since a cursor only belongs to one sort order.
 */
export function useSortParams<F extends string>(allowed: readonly F[], defaults: SortState<F>) {
  const [params, setParams] = useSearchParams();
  const rawBy = params.get("sort_by");
  const rawOrder = params.get("sort_order")?.toUpperCase();

  const sort: SortState<F> = {
    sortBy: rawBy !== null && (allowed as readonly string[]).includes(rawBy) ? (rawBy as F) : defaults.sortBy,
    sortOrder: rawOrder === "ASC" || rawOrder === "DESC" ? rawOrder : defaults.sortOrder,
  };

  function setSort(next: SortState<F>) {
    setParams(
      (prev) => {
        const p = new URLSearchParams(prev);
        p.delete("cursor");
        if (next.sortBy === defaults.sortBy && next.sortOrder === defaults.sortOrder) {
          p.delete("sort_by");
          p.delete("sort_order");
        } else {
          p.set("sort_by", next.sortBy);
          p.set("sort_order", next.sortOrder);
        }
        return p;
      },
      { replace: true },
    );
  }

  return [sort, setSort] as const;
}
