import type { ArtifactColumnId } from "@/components/artifacts/columns";
import type { SortState } from "@/hooks/use-sort-params";

/** Sortable table columns (all are API sort fields). */
export const ARTIFACT_TABLE_SORT_FIELDS: readonly ArtifactColumnId[] = [
  "full_url",
  "response_content_type",
  "response_status_code",
  "response_body_size_bytes",
  "requested_at",
];

/** The API's default order: newest first. */
export const DEFAULT_ARTIFACT_SORT: SortState<ArtifactColumnId> = { sortBy: "requested_at", sortOrder: "DESC" };
