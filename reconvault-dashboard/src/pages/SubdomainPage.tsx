import { GlobeIcon } from "lucide-react";
import type { ReactNode } from "react";
import { Link, useParams } from "react-router";

import { EmptyState } from "@/components/common/EmptyState";
import { ErrorState } from "@/components/common/ErrorState";
import { TableSkeleton } from "@/components/common/LoadingSkeleton";
import { Panel } from "@/components/common/Panel";
import { PageHeader } from "@/components/layout/PageHeader";
import { Button } from "@/components/ui/button";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { useSubdomains } from "@/features/recon/api";
import { formatInteger } from "@/lib/formatters";
import { hostPath, routes } from "@/lib/routes";
import { isValidHostnameParam } from "@/lib/validation";

export function SubdomainPage() {
  const { root_domain: raw } = useParams();
  if (!isValidHostnameParam(raw)) {
    return (
      <>
        <PageHeader title="Invalid root domain" breadcrumbs={[{ label: "Home", to: routes.home }, { label: "Root domain" }]} />
        <EmptyState
          icon={GlobeIcon}
          title="This root domain is not valid"
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
  // The API stores names lower-cased.
  return <Subdomains rootDomain={raw.trim().toLowerCase()} />;
}

function Subdomains({ rootDomain }: { rootDomain: string }) {
  const { data, error, isPending, refetch } = useSubdomains(rootDomain);
  const hosts = data?.data;

  let body: ReactNode;
  if (isPending) {
    body = <TableSkeleton rows={6} columns={2} />;
  } else if (error) {
    body = <ErrorState error={error} title="Failed to load subdomains" onRetry={() => void refetch()} />;
  } else if (!hosts || hosts.length === 0) {
    body = (
      <EmptyState icon={GlobeIcon} title="No hostnames found" description="DNS autoscan hasn't recorded any hostname under this domain." />
    );
  } else {
    body = (
      <Table>
        <caption className="sr-only">Hostnames under {rootDomain}</caption>
        <TableHeader className="bg-muted/25">
          <TableRow className="hover:bg-transparent">
            <TableHead>Hostname</TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          {hosts.map((h) => (
            <TableRow key={h.hostname}>
              <TableCell className="max-w-0">
                <Link
                  to={hostPath(h.hostname)}
                  className="block truncate rounded-sm font-mono text-[13px] text-primary underline-offset-4 outline-none hover:underline focus-visible:ring-2 focus-visible:ring-ring/60"
                  title={h.hostname}
                >
                  {h.hostname}
                </Link>
              </TableCell>
            </TableRow>
          ))}
        </TableBody>
      </Table>
    );
  }

  return (
    <>
      <PageHeader
        mono
        title={rootDomain}
        breadcrumbs={[{ label: "Home", to: routes.home }, { label: rootDomain }]}
        description={
          data ? `${formatInteger(data.meta.total)} unique ${data.meta.total === 1 ? "hostname" : "hostnames"}` : undefined
        }
      />
      <Panel id="subdomains" title="Hostnames" description="Sorted alphabetically" flush>
        {body}
      </Panel>
    </>
  );
}
