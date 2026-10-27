import { useSearchParams } from "react-router";

/** Page size in the URL (`limit`); only values from `options` are accepted. */
export function useLimitParam(options: readonly number[], fallback: number) {
  const [params, setParams] = useSearchParams();
  const parsed = Number(params.get("limit"));
  const limit = options.includes(parsed) ? parsed : fallback;

  function setLimit(next: number) {
    setParams((prev) => {
      const p = new URLSearchParams(prev);
      p.delete("cursor"); // a new page size starts from the first page
      if (next === fallback) p.delete("limit");
      else p.set("limit", String(next));
      return p;
    });
  }

  return [limit, setLimit] as const;
}
