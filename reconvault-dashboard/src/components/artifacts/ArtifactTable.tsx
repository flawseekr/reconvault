import {
  flexRender,
  getCoreRowModel,
  getSortedRowModel,
  type SortingState,
  useReactTable,
} from "@tanstack/react-table";
import { ArrowDownIcon, ArrowUpDownIcon, ArrowUpIcon } from "lucide-react";
import type { MouseEvent, ReactNode } from "react";
import { useNavigate } from "react-router";

import { type ArtifactColumnId, artifactColumns } from "@/components/artifacts/columns";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { artifactPath } from "@/lib/routes";
import { cn } from "@/lib/utils";
import type { ArtifactSummary, SortOrder } from "@/types/api";

export interface ArtifactSort {
  sortBy: ArtifactColumnId;
  sortOrder: SortOrder;
}

interface ArtifactTableProps {
  data: ArtifactSummary[];
  sort: ArtifactSort;
  onSortChange: (sort: ArtifactSort) => void;
  /**
   * "server": rows arrive sorted from the API and a sort change triggers a
   * new request. "client": the loaded rows are sorted in the browser.
   */
  sortMode: "server" | "client";
  caption: string;
  /** Rendered in place of rows when `data` is empty. */
  empty: ReactNode;
  /** Dims rows while a new page or sort order is loading. */
  isUpdating?: boolean;
  /** "path" drops scheme and host from the Endpoint column (all rows share one host). */
  endpointDisplay?: "url" | "path";
}

const INTERACTIVE = "a, button, input, select, textarea, [role='button'], [data-row-ignore]";

export function ArtifactTable({ data, sort, onSortChange, sortMode, caption, empty, isUpdating, endpointDisplay = "url" }: ArtifactTableProps) {
  const navigate = useNavigate();
  const sorting: SortingState = [{ id: sort.sortBy, desc: sort.sortOrder === "DESC" }];

  // React Compiler is not used in this project, so its warning that
  // TanStack Table's API cannot be auto-memoized does not apply.
  // eslint-disable-next-line react-hooks/incompatible-library
  const table = useReactTable({
    data,
    columns: artifactColumns,
    state: { sorting },
    manualSorting: sortMode === "server",
    getCoreRowModel: getCoreRowModel(),
    getSortedRowModel: sortMode === "client" ? getSortedRowModel() : undefined,
    getRowId: (row) => row.id,
    meta: { endpointDisplay },
  });

  function toggleSort(id: ArtifactColumnId) {
    if (id === sort.sortBy) onSortChange({ sortBy: id, sortOrder: sort.sortOrder === "ASC" ? "DESC" : "ASC" });
    else onSortChange({ sortBy: id, sortOrder: "ASC" });
  }

  function onRowClick(event: MouseEvent<HTMLTableRowElement>, id: string) {
    if (event.button !== 0 || event.metaKey || event.ctrlKey || event.shiftKey || event.altKey) return;
    if ((event.target as HTMLElement).closest(INTERACTIVE)) return; // controls handle their own clicks
    if (window.getSelection()?.toString()) return; // the user is selecting text
    navigate(artifactPath(id));
  }

  const rows = table.getRowModel().rows;

  return (
    <>
      <Table className={cn("table-fixed transition-opacity", isUpdating && "opacity-60")} aria-busy={isUpdating || undefined}>
        <caption className="sr-only">
          {caption}
          {sortMode === "client" ? " Sorting applies to the loaded results only." : ""}
        </caption>
        <colgroup>
          <col />
          <col className="hidden w-52 xl:table-column" />
          <col className="w-20" />
          <col className="hidden w-24 sm:table-column" />
          <col className="hidden w-48 md:table-column" />
        </colgroup>
        <TableHeader className="bg-muted/25">
          {table.getHeaderGroups().map((group) => (
            <TableRow key={group.id} className="hover:bg-transparent">
              {group.headers.map((header) => {
                const id = header.column.id as ArtifactColumnId;
                const active = id === sort.sortBy;
                const label = String(header.column.columnDef.header);
                const Icon = !active ? ArrowUpDownIcon : sort.sortOrder === "ASC" ? ArrowUpIcon : ArrowDownIcon;
                return (
                  <TableHead
                    key={header.id}
                    aria-sort={active ? (sort.sortOrder === "ASC" ? "ascending" : "descending") : "none"}
                    className={cn(header.column.columnDef.meta?.className, header.column.columnDef.meta?.align === "right" && "text-right")}
                  >
                    <button
                      type="button"
                      onClick={() => toggleSort(id)}
                      className={cn(
                        "-mx-1.5 inline-flex items-center gap-1 rounded px-1.5 py-1 transition-colors outline-none hover:text-foreground focus-visible:ring-2 focus-visible:ring-ring/60",
                        active && "text-foreground",
                      )}
                    >
                      {label}
                      <Icon className={cn("size-3", !active && "opacity-40")} aria-hidden="true" />
                      <span className="sr-only">
                        {active ? `, sorted ${sort.sortOrder === "ASC" ? "ascending" : "descending"}. ` : ". "}
                        Activate to sort {active && sort.sortOrder === "ASC" ? "descending" : "ascending"}.
                      </span>
                    </button>
                  </TableHead>
                );
              })}
            </TableRow>
          ))}
        </TableHeader>
        <TableBody>
          {rows.map((row) => (
            <TableRow
              key={row.id}
              onClick={(e) => onRowClick(e, row.original.id)}
              className="cursor-pointer hover:bg-accent/50"
            >
              {row.getVisibleCells().map((cell) => (
                <TableCell
                  key={cell.id}
                  className={cn(
                    "overflow-hidden",
                    cell.column.columnDef.meta?.className,
                    cell.column.columnDef.meta?.align === "right" && "text-right",
                  )}
                >
                  {flexRender(cell.column.columnDef.cell, cell.getContext())}
                </TableCell>
              ))}
            </TableRow>
          ))}
        </TableBody>
      </Table>
      {/* Outside the table: a cell spanning the responsively hidden columns
          collapses the fixed-layout column widths. */}
      {rows.length === 0 && empty}
    </>
  );
}
