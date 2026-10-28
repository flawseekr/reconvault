import { SearchIcon } from "lucide-react";
import { useSearchParams } from "react-router";

import { ArtifactListView } from "@/components/artifacts/ArtifactListView";
import { ARTIFACT_TABLE_SORT_FIELDS, DEFAULT_ARTIFACT_SORT } from "@/components/artifacts/sort";
import { EmptyState } from "@/components/common/EmptyState";
import { Panel } from "@/components/common/Panel";
import { PageHeader } from "@/components/layout/PageHeader";
import { SearchBar } from "@/components/search/SearchBar";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { useArtifactSearch } from "@/features/search/api";
import { useLimitParam } from "@/hooks/use-limit-param";
import { useSortParams } from "@/hooks/use-sort-params";
import { routes } from "@/lib/routes";
import { validateSearchQuery } from "@/lib/validation";

const RESULT_LIMITS = [10, 30, 50, 100] as const;
const DEFAULT_LIMIT = 30;

export function SearchPage() {
  const [params, setParams] = useSearchParams();
  const q = params.get("q") ?? "";
  const problem = q ? validateSearchQuery(q) : null;
  const activeQuery = q && !problem ? q : null;

  const [limit, setLimit] = useLimitParam(RESULT_LIMITS, DEFAULT_LIMIT);
  // The search API has no sort parameters: results arrive newest first and
  // column sorting reorders the loaded results in the browser.
  const [sort, setSort] = useSortParams(ARTIFACT_TABLE_SORT_FIELDS, DEFAULT_ARTIFACT_SORT);
  const search = useArtifactSearch(activeQuery, limit);
  const rows = search.data?.data;

  function onSearch(next: string) {
    setParams((prev) => {
      const p = new URLSearchParams(prev);
      p.set("q", next);
      return p;
    });
  }

  return (
    <>
      <PageHeader
        title="Search"
        breadcrumbs={[{ label: "Home", to: routes.home }, { label: "Search" }]}
        description="Case-insensitive substring match across all artifact fields"
      />

      <div className="grid gap-6">
        {/* Remount on URL change so back/forward restores the input text. */}
        <SearchBar key={q} defaultValue={q} onSearch={onSearch} size="lg" autoFocus={!q} />

        {problem ? (
          <EmptyState icon={SearchIcon} title="Invalid search" description={problem} />
        ) : !activeQuery ? (
          <EmptyState
            icon={SearchIcon}
            title="Search HTTP artifacts"
            description="Matches URLs, headers, bodies, status codes, IDs and timestamps. %, _ and \ match literally."
          />
        ) : (
          <Panel
            id="search-results"
            flush
            title={
              rows ? (
                <>
                  {rows.length} {rows.length === 1 ? "match" : "matches"} for{" "}
                  <span className="font-mono text-primary">“{search.data?.meta.query}”</span>
                </>
              ) : (
                "Results"
              )
            }
            description={
              rows && rows.length === limit
                ? `Showing the ${limit} newest matches. Narrow the query or raise the limit.`
                : "Newest first. Sorting applies to these results only."
            }
            actions={
              <div className="flex items-center gap-2 text-xs text-muted-foreground">
                <span id="result-limit-label">Limit</span>
                <Select value={String(limit)} onValueChange={(v) => setLimit(Number(v))}>
                  <SelectTrigger aria-labelledby="result-limit-label" className="h-7 text-xs">
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    {RESULT_LIMITS.map((n) => (
                      <SelectItem key={n} value={String(n)}>
                        {n}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              </div>
            }
          >
            <ArtifactListView
              rows={rows}
              isPending={search.isPending}
              error={search.error}
              isPlaceholderData={search.isPlaceholderData}
              onRetry={() => void search.refetch()}
              sort={sort}
              onSortChange={setSort}
              sortMode="client"
              caption={`Search results for ${activeQuery}`}
              emptyTitle="No matches"
              emptyDescription="No artifact contains this text."
            />
          </Panel>
        )}
      </div>
    </>
  );
}
