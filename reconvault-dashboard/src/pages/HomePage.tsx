import { RefreshCwIcon } from "lucide-react";
import { useNavigate } from "react-router";

import { ArtifactListView } from "@/components/artifacts/ArtifactListView";
import { ARTIFACT_TABLE_SORT_FIELDS, DEFAULT_ARTIFACT_SORT } from "@/components/artifacts/sort";
import { Panel } from "@/components/common/Panel";
import { HostList } from "@/components/hosts/HostList";
import { PageHeader } from "@/components/layout/PageHeader";
import { RootDomainList } from "@/components/recon/RootDomainList";
import { RequestBar } from "@/components/requests/RequestBar";
import { SearchBar } from "@/components/search/SearchBar";
import { Button } from "@/components/ui/button";
import { useArtifactList } from "@/features/artifacts/api";
import { useSortParams } from "@/hooks/use-sort-params";
import { searchPath } from "@/lib/routes";
import { cn } from "@/lib/utils";

const HOME_LIMIT = 50;

export function HomePage() {
  const navigate = useNavigate();
  const [sort, setSort] = useSortParams(ARTIFACT_TABLE_SORT_FIELDS, DEFAULT_ARTIFACT_SORT);
  const list = useArtifactList({ ...sort, limit: HOME_LIMIT });

  return (
    <>
      <PageHeader title="Overview" description="Recorded HTTP exchanges and the hosts they target." />

      <div className="grid gap-6">
        <div className="grid gap-4 lg:grid-cols-2">
          <Panel title="Search" description="Case-insensitive search across URLs, headers and bodies.">
            <SearchBar onSearch={(q) => navigate(searchPath(q))} />
          </Panel>
          <Panel title="Request a URL" description="POST /api/v1/http/request">
            <RequestBar />
          </Panel>
        </div>

        <Panel
          id="artifacts"
          title="HTTP artifacts"
          description={`Latest ${HOME_LIMIT} by the selected sort order`}
          flush
          actions={
            <Button
              variant="ghost"
              size="icon-sm"
              onClick={() => void list.refetch()}
              disabled={list.isFetching}
              aria-label="Refresh artifacts"
            >
              <RefreshCwIcon className={cn(list.isFetching && "animate-spin")} />
            </Button>
          }
        >
          <ArtifactListView
            rows={list.data?.data}
            isPending={list.isPending}
            error={list.error}
            isPlaceholderData={list.isPlaceholderData}
            onRetry={() => void list.refetch()}
            sort={sort}
            onSortChange={setSort}
            sortMode="server"
            caption="Recorded HTTP artifacts"
            emptyTitle="No artifacts yet"
            emptyDescription="Request a URL above to record the first exchange."
          />
        </Panel>

        <Panel id="hosts" title="Recently scanned hosts" description="Most recently requested first">
          <HostList />
        </Panel>

        <Panel id="root-domains" title="Root Domains Overview" description="Unique hostnames per root domain, from the DNS autoscan">
          <RootDomainList />
        </Panel>
      </div>
    </>
  );
}
