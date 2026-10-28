import { NetworkIcon } from "lucide-react";
import { Link } from "react-router";

import { EmptyState } from "@/components/common/EmptyState";
import { ErrorState } from "@/components/common/ErrorState";
import { Skeleton } from "@/components/ui/skeleton";
import { useRootDomains } from "@/features/recon/api";
import { formatInteger } from "@/lib/formatters";
import { subdomainPath } from "@/lib/routes";

/** Available subdomains grouped by root domain, largest group first. */
export function RootDomainList() {
  const { data, error, isPending, refetch } = useRootDomains();

  if (isPending) {
    return (
      <div role="status" aria-label="Loading subdomains" className="flex flex-wrap gap-2">
        {Array.from({ length: 6 }, (_, i) => (
          <Skeleton key={i} className="h-9 w-40 rounded-lg" />
        ))}
      </div>
    );
  }
  if (error) return <ErrorState error={error} title="Failed to load subdomains" onRetry={() => void refetch()} />;
  if (data.data.length === 0) {
    return (
      <EmptyState
        icon={NetworkIcon}
        title="No subdomains found"
        description="Subdomains appear after DNS autoscan resolves a hostname."
      />
    );
  }

  return (
    <ul className="flex flex-wrap gap-2">
      {data.data.map((d) => (
        <li key={d.root_domain} className="max-w-full min-w-0">
          <Link
            to={subdomainPath(d.root_domain)}
            className="group flex min-w-0 items-center gap-2 rounded-lg border border-border bg-card px-3 py-1.5 transition-colors outline-none hover:border-primary/40 hover:bg-accent/40 focus-visible:ring-2 focus-visible:ring-ring/60"
          >
            <span className="min-w-0 truncate font-mono text-[13px] font-medium" title={d.root_domain}>
              {d.root_domain}
            </span>
            <span className="inline-flex shrink-0 items-center rounded border border-border bg-muted/40 px-1.5 font-mono text-[11px] tabular-nums text-muted-foreground">
              {formatInteger(d.hostname_count)} {d.hostname_count === 1 ? "subdomain" : "subdomains"}
            </span>
          </Link>
        </li>
      ))}
    </ul>
  );
}
