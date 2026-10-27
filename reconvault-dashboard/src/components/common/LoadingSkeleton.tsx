import { Skeleton } from "@/components/ui/skeleton";

/** Placeholder rows matching the artifact table's density. */
export function TableSkeleton({ rows = 8, columns = 5 }: { rows?: number; columns?: number }) {
  return (
    <div role="status" aria-label="Loading" className="divide-y divide-border/70">
      {Array.from({ length: rows }, (_, r) => (
        <div key={r} className="flex items-center gap-4 px-3 py-2.5">
          <Skeleton className="h-4 w-12" />
          <Skeleton className="h-4 flex-1" />
          {Array.from({ length: columns - 2 }, (_, c) => (
            <Skeleton key={c} className="hidden h-4 w-20 md:block" />
          ))}
        </div>
      ))}
    </div>
  );
}

export function BlockSkeleton({ lines = 6 }: { lines?: number }) {
  return (
    <div role="status" aria-label="Loading" className="space-y-2.5">
      {Array.from({ length: lines }, (_, i) => (
        <Skeleton key={i} className="h-4" style={{ width: `${90 - ((i * 17) % 45)}%` }} />
      ))}
    </div>
  );
}
