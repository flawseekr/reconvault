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
      <PageHeader title="Overview" description="Monitor hosts, HTTP responses and DNS records." />

      <div className="grid gap-6">
        <div className="grid gap-6 xl:grid-cols-2">
          <Panel title="Search" description="Case-insensitive match on URLs, headers and bodies">
            <SearchBar onSearch={(q) => navigate(searchPath(q))} />
          </Panel>
          <Panel title="Probe a URL" description="Sends a GET request and records the response">
            <RequestBar />
          </Panel>
        </div>

        <Panel
          id="artifacts"
          title="HTTP artifacts"
          description={`Latest ${HOME_LIMIT}, sorted by the selected column`}
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
            caption="HTTP artifacts"
            emptyTitle="No artifacts yet"
            emptyDescription="Probe a URL above to record one."
          />
        </Panel>

        <Panel id="hosts" title="Recently probed HTTP" description="Hosts, most recently probed first">
          <HostList />
        </Panel>

        <Panel id="available-subdomains" title="Available subdomains" description="Subdomains found by DNS autoscan, grouped by root domain">
          <RootDomainList />
        </Panel>
      </div>
    </>
  );
}
