import { InboxIcon } from "lucide-react";
import type { ReactNode } from "react";

import { type ArtifactSort, ArtifactTable } from "@/components/artifacts/ArtifactTable";
import { EmptyState } from "@/components/common/EmptyState";
import { ErrorState } from "@/components/common/ErrorState";
import { TableSkeleton } from "@/components/common/LoadingSkeleton";
import type { ArtifactSummary } from "@/types/api";

interface ArtifactListViewProps {
  rows: ArtifactSummary[] | undefined;
  isPending: boolean;
  error: unknown;
  isPlaceholderData?: boolean;
  onRetry: () => void;
  sort: ArtifactSort;
  onSortChange: (sort: ArtifactSort) => void;
  sortMode: "server" | "client";
  caption: string;
  emptyTitle: string;
  emptyDescription?: ReactNode;
  /** Rendered on errors in addition to the retry button (e.g. reset paging). */
  errorAction?: ReactNode;
}

/** Artifact table with the shared loading, error and empty states. */
export function ArtifactListView(props: ArtifactListViewProps) {
  const { rows, isPending, error, isPlaceholderData, onRetry, emptyTitle, emptyDescription, errorAction, ...table } = props;

  if (isPending) return <TableSkeleton />;
  if (error && !rows) {
    return (
      <div>
        <ErrorState error={error} title="Could not load artifacts" onRetry={onRetry} />
        {errorAction && <div className="-mt-8 flex justify-center pb-8">{errorAction}</div>}
      </div>
    );
  }
  return (
    <ArtifactTable
      {...table}
      data={rows ?? []}
      isUpdating={isPlaceholderData}
      empty={<EmptyState icon={InboxIcon} title={emptyTitle} description={emptyDescription} />}
    />
  );
}
