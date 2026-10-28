import type { ColumnDef } from "@tanstack/react-table";
import { Link } from "react-router";

import { BodySize } from "@/components/artifacts/BodySize";
import { MethodBadge } from "@/components/artifacts/MethodBadge";
import { StatusBadge } from "@/components/artifacts/StatusBadge";
import { TimestampDisplay } from "@/components/artifacts/TimestampDisplay";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import { middleTruncate } from "@/lib/formatters";
import { artifactPath } from "@/lib/routes";
import type { ArtifactSortField, ArtifactSummary } from "@/types/api";

/** Column ids double as the API's sort fields. */
export type ArtifactColumnId = Extract<
  ArtifactSortField,
  "full_url" | "response_content_type" | "response_status_code" | "response_body_size_bytes" | "requested_at"
>;

const URL_DISPLAY_CHARS = 96;

// Client-side sort accessors: null becomes undefined so TanStack keeps
// missing values last, and timestamps compare as instants rather than
// strings (fractional seconds vary in length).
export const artifactColumns: ColumnDef<ArtifactSummary>[] = [
  {
    id: "full_url",
    header: "Endpoint",
    accessorFn: (row) => row.full_url,
    sortingFn: "text",
    cell: ({ row }) => {
      const a = row.original;
      return (
        <div className="flex min-w-0 items-center gap-2.5">
          <MethodBadge method={a.http_method} />
          <Tooltip>
            <TooltipTrigger asChild>
              <Link
                to={artifactPath(a.id)}
                // The visible URL may be truncated; announce the full endpoint.
                aria-label={`${a.http_method} ${a.full_url}`}
                className="min-w-0 truncate rounded-sm font-mono text-[13px] text-foreground/90 outline-none hover:text-primary focus-visible:ring-2 focus-visible:ring-ring/60"
              >
                {middleTruncate(a.full_url, URL_DISPLAY_CHARS)}
              </Link>
            </TooltipTrigger>
            <TooltipContent align="start">
              <span className="font-mono">{a.full_url}</span>
            </TooltipContent>
          </Tooltip>
        </div>
      );
    },
  },
  {
    id: "response_content_type",
    header: "Content type",
    accessorFn: (row) => row.response_content_type ?? undefined,
    sortUndefined: "last",
    sortingFn: "text",
    meta: { className: "hidden xl:table-cell" },
    cell: ({ row }) => {
      const ct = row.original.response_content_type;
      return ct ? (
        <span className="block max-w-56 truncate font-mono text-xs text-muted-foreground" title={ct}>
          {ct}
        </span>
      ) : (
        <span className="text-muted-foreground">—</span>
      );
    },
  },
  {
    id: "response_status_code",
    header: "Status",
    accessorFn: (row) => row.response_status_code ?? undefined,
    sortUndefined: "last",
    cell: ({ row }) => <StatusBadge code={row.original.response_status_code} />,
  },
  {
    id: "response_body_size_bytes",
    header: "Size",
    accessorFn: (row) => row.response_body_size_bytes ?? undefined,
    sortUndefined: "last",
    meta: { className: "hidden sm:table-cell", align: "right" },
    cell: ({ row }) => <BodySize bytes={row.original.response_body_size_bytes} />,
  },
  {
    id: "requested_at",
    header: "Requested",
    accessorFn: (row) => Date.parse(row.requested_at),
    meta: { className: "hidden md:table-cell" },
    cell: ({ row }) => <TimestampDisplay value={row.original.requested_at} />,
  },
];
