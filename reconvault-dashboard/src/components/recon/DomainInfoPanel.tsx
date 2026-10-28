import { ArrowRightIcon, RadarIcon } from "lucide-react";
import type { ReactNode } from "react";
import { Link } from "react-router";

import { TimestampDisplay } from "@/components/artifacts/TimestampDisplay";
import { EmptyState } from "@/components/common/EmptyState";
import { ErrorState } from "@/components/common/ErrorState";
import { Panel } from "@/components/common/Panel";
import { Skeleton } from "@/components/ui/skeleton";
import { useDomainInfo } from "@/features/recon/api";
import { ApiError } from "@/lib/api-client";
import { subdomainPath } from "@/lib/routes";
import { cn } from "@/lib/utils";
import type { DomainInfo } from "@/types/api";

/** Root domain, WAF and ASN for a hostname, from the DNS autoscan. */
export function DomainInfoPanel({ hostname }: { hostname: string }) {
  const { data, error, isPending, refetch } = useDomainInfo(hostname);
  const notScanned = error instanceof ApiError && error.isNotFound;

  let body: ReactNode;
  if (isPending) {
    body = (
      <div role="status" aria-label="Loading domain info" className="grid grid-cols-2 gap-3 sm:grid-cols-3">
        {Array.from({ length: 3 }, (_, i) => (
          <Skeleton key={i} className={cn("h-[92px] rounded-lg", i === 0 && "col-span-2 sm:col-span-1")} />
        ))}
      </div>
    );
  } else if (notScanned) {
    body = (
      <EmptyState
        icon={RadarIcon}
        title="Not scanned yet"
        description="DNS autoscan hasn't resolved this hostname. Domain info appears after its next run."
        className="py-6"
      />
    );
  } else if (error) {
    body = <ErrorState error={error} title="Failed to load domain info" onRetry={() => void refetch()} className="py-6" />;
  } else {
    body = <DomainStats domain={data} />;
  }

  return (
    <Panel
      id="domain-info"
      title="Domain info"
      description={data ? <span>Last scanned <TimestampDisplay value={data.updated_at} variant="relative" /></span> : undefined}
    >
      {body}
    </Panel>
  );
}

function DomainStats({ domain }: { domain: DomainInfo }) {
  return (
    <dl className="grid grid-cols-2 gap-3 sm:grid-cols-3">
      <Stat label="Root domain" className="col-span-2 sm:col-span-1">
        <dd className="truncate font-mono text-lg font-semibold sm:text-xl" title={domain.root_domain}>
          {domain.root_domain}
        </dd>
        <dd>
          <Link
            to={subdomainPath(domain.root_domain)}
            className="inline-flex items-center gap-1 rounded-sm text-xs text-primary underline-offset-4 outline-none hover:underline focus-visible:ring-2 focus-visible:ring-ring/60"
          >
            View subdomains <ArrowRightIcon className="size-3" aria-hidden="true" />
          </Link>
        </dd>
      </Stat>
      <Stat label="WAF present">
        <dd
          className={cn(
            "text-xl font-semibold",
            domain.waf === null && "text-muted-foreground",
          )}
        >
          {domain.waf === null ? "Unknown" : domain.waf ? "Yes" : "No"}
        </dd>
      </Stat>
      <Stat label="ASN">
        <dd className={cn("font-mono text-xl font-semibold tabular-nums", domain.asn === null && "text-muted-foreground")}>
          {domain.asn === null ? "Unknown" : domain.asn}
        </dd>
        <dd className="line-clamp-2 text-xs break-words text-muted-foreground" title={domain.asn_name ?? undefined}>
          {domain.asn_name ?? "No ASN name"}
        </dd>
      </Stat>
    </dl>
  );
}

function Stat({ label, children, className }: { label: string; children: ReactNode; className?: string }) {
  return (
    <div className={cn("grid min-w-0 content-start gap-1 rounded-lg border border-border bg-muted/20 px-4 py-3", className)}>
      <dt className="text-xs text-muted-foreground">{label}</dt>
      {children}
    </div>
  );
}
