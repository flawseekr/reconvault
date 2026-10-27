import { ChevronLeftIcon, ChevronRightIcon } from "lucide-react";

import { Button } from "@/components/ui/button";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";

interface PaginationControlsProps {
  /** 1-based page number, or null when unknown (shared deep link). */
  page: number | null;
  pageSize: number;
  pageSizeOptions: readonly number[];
  itemCount: number;
  hasPrevious: boolean;
  hasNext: boolean;
  disabled?: boolean;
  onPrevious: () => void;
  onNext: () => void;
  onPageSizeChange: (size: number) => void;
}

/** Previous/next paging; the API exposes no total count, so none is shown. */
export function PaginationControls({
  page,
  pageSize,
  pageSizeOptions,
  itemCount,
  hasPrevious,
  hasNext,
  disabled,
  onPrevious,
  onNext,
  onPageSizeChange,
}: PaginationControlsProps) {
  return (
    <nav aria-label="Pagination" className="flex flex-wrap items-center justify-between gap-3 px-3 py-2.5 text-xs text-muted-foreground">
      <div className="flex items-center gap-2">
        <span id="page-size-label">Rows per page</span>
        <Select value={String(pageSize)} onValueChange={(v) => onPageSizeChange(Number(v))} disabled={disabled}>
          <SelectTrigger aria-labelledby="page-size-label" className="h-7 text-xs">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            {pageSizeOptions.map((n) => (
              <SelectItem key={n} value={String(n)}>
                {n}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      </div>
      <div className="flex items-center gap-3">
        <span className="tabular-nums" aria-live="polite">
          {page === null ? "Page —" : `Page ${page}`} · {itemCount} {itemCount === 1 ? "row" : "rows"}
        </span>
        <div className="flex gap-1">
          <Button variant="outline" size="icon-sm" onClick={onPrevious} disabled={disabled || !hasPrevious} aria-label="Previous page">
            <ChevronLeftIcon />
          </Button>
          <Button variant="outline" size="icon-sm" onClick={onNext} disabled={disabled || !hasNext} aria-label="Next page">
            <ChevronRightIcon />
          </Button>
        </div>
      </div>
    </nav>
  );
}
