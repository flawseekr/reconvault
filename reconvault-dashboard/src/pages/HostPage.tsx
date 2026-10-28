import { GlobeIcon } from "lucide-react";
import { Link, useParams } from "react-router";

import { ArtifactListView } from "@/components/artifacts/ArtifactListView";
import { ARTIFACT_TABLE_SORT_FIELDS, DEFAULT_ARTIFACT_SORT } from "@/components/artifacts/sort";
import { TimestampDisplay } from "@/components/artifacts/TimestampDisplay";
import { EmptyState } from "@/components/common/EmptyState";
import { PaginationControls } from "@/components/common/PaginationControls";
import { Panel } from "@/components/common/Panel";
import { DnsHistoryPanel } from "@/components/recon/DnsHistoryPanel";
import { DomainInfoPanel } from "@/components/recon/DomainInfoPanel";
import { PageHeader } from "@/components/layout/PageHeader";
import { Button } from "@/components/ui/button";
import { useArtifactList } from "@/features/artifacts/api";
import { useDomains } from "@/features/hosts/api";
import { useCursorPagination } from "@/hooks/use-cursor-pagination";
import { useLimitParam } from "@/hooks/use-limit-param";
import { useSortParams } from "@/hooks/use-sort-params";
import { ApiError } from "@/lib/api-client";
import { formatInteger } from "@/lib/formatters";
import { routes } from "@/lib/routes";
import { isValidHostnameParam } from "@/lib/validation";

const PAGE_SIZES = [25, 50, 100] as const;

export function HostPage() {
  const { hostname: raw } = useParams();
  if (!isValidHostnameParam(raw)) {
    return (
      <>
        <PageHeader title="Invalid hostname" breadcrumbs={[{ label: "Home", to: routes.home }, { label: "Host" }]} />
        <EmptyState
          icon={GlobeIcon}
          title="This hostname is not valid"
          description="Use 1 to 255 characters with no control characters."
          action={
            <Button asChild variant="outline" size="sm">
              <Link to={routes.home}>Back to Home</Link>
            </Button>
          }
        />
      </>
    );
  }
  // The API stores hostnames lower-cased; keying on the normalized value
  // also remounts (and resets paging) when the host changes.
  const hostname = raw.trim().toLowerCase();
  return <HostArtifacts key={hostname} hostname={hostname} />;
}

function HostArtifacts({ hostname }: { hostname: string }) {
  const [sort, setSort] = useSortParams(ARTIFACT_TABLE_SORT_FIELDS, DEFAULT_ARTIFACT_SORT);
  const [limit, setLimit] = useLimitParam(PAGE_SIZES, PAGE_SIZES[0]);
  const pagination = useCursorPagination();
  const list = useArtifactList({ ...sort, limit, hostname, cursor: pagination.cursor });
  const domain = useDomains().data?.data.find((d) => d.hostname === hostname);

  const cursorRejected = list.error instanceof ApiError && list.error.field === "cursor";
  const rows = list.data?.data;

  return (
    <>
      <PageHeader
        mono
        title={hostname}
        breadcrumbs={[{ label: "Home", to: routes.home }, { label: hostname }]}
        description={
          domain ? (
            <span className="inline-flex flex-wrap items-center gap-x-1.5">
              {formatInteger(domain.artifact_count)} {domain.artifact_count === 1 ? "artifact" : "artifacts"} · last request{" "}
              <TimestampDisplay value={domain.last_requested_at} variant="relative" />
            </span>
          ) : undefined
        }
      />

      <div className="grid gap-6">
        <DomainInfoPanel hostname={hostname} />

        <Panel id="host-artifacts" title="HTTP artifacts" description="All recorded requests to this host" flush>
          <ArtifactListView
            rows={rows}
            isPending={list.isPending}
            error={list.error}
            isPlaceholderData={list.isPlaceholderData}
            onRetry={() => void list.refetch()}
            sort={sort}
            onSortChange={setSort}
            sortMode="server"
            caption={`HTTP artifacts for ${hostname}`}
            emptyTitle={pagination.cursor ? "No more artifacts" : "No artifacts for this host"}
            emptyDescription={pagination.cursor ? undefined : "Probe a URL on this host from Home to record one."}
            errorAction={
              cursorRejected ? (
                <Button variant="outline" size="sm" onClick={pagination.goFirst}>
                  Back to first page
                </Button>
              ) : undefined
            }
          />
          {rows && (
            <div className="border-t border-border">
              <PaginationControls
                page={pagination.page}
                pageSize={limit}
                pageSizeOptions={PAGE_SIZES}
                itemCount={rows.length}
                hasPrevious={pagination.hasPrevious}
                hasNext={list.data?.meta.next_cursor != null}
                disabled={list.isPlaceholderData}
                onPrevious={pagination.goPrevious}
                onNext={() => {
                  const next = list.data?.meta.next_cursor;
                  if (next) pagination.goNext(next);
                }}
                onPageSizeChange={setLimit}
              />
            </div>
          )}
        </Panel>

        <DnsHistoryPanel hostname={hostname} />
      </div>
    </>
  );
}
