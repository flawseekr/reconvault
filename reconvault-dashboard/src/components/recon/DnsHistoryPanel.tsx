import { NetworkIcon } from "lucide-react";
import type { ReactNode } from "react";

import { TimestampDisplay } from "@/components/artifacts/TimestampDisplay";
import { EmptyState } from "@/components/common/EmptyState";
import { ErrorState } from "@/components/common/ErrorState";
import { TableSkeleton } from "@/components/common/LoadingSkeleton";
import { Panel } from "@/components/common/Panel";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { useDnsHistory } from "@/features/recon/api";
import { formatInteger } from "@/lib/formatters";

/** Every stored DNS record version for a hostname, newest first (server order). */
export function DnsHistoryPanel({ hostname }: { hostname: string }) {
  const { data, error, isPending, refetch } = useDnsHistory(hostname);
  const records = data?.data;

  let body: ReactNode;
  if (isPending) {
    body = <TableSkeleton rows={4} columns={4} />;
  } else if (error) {
    body = <ErrorState error={error} title="Could not load DNS history" onRetry={() => void refetch()} />;
  } else if (!records || records.length === 0) {
    body = (
      <EmptyState
        icon={NetworkIcon}
        title="No DNS records"
        description="The DNS autoscan has not stored any records for this hostname."
      />
    );
  } else {
    body = (
      // Histories are unpaginated; cap the height so the artifacts stay reachable.
      <div className="max-h-[28rem] overflow-y-auto">
        <Table>
          <caption className="sr-only">DNS history for {hostname}</caption>
          <TableHeader className="sticky top-0 z-10 bg-card">
            <TableRow className="hover:bg-transparent">
              <TableHead className="w-20">Type</TableHead>
              <TableHead>Value</TableHead>
              <TableHead className="w-24 text-right">TTL</TableHead>
              <TableHead className="w-44">Updated</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {records.map((r) => (
              <TableRow key={`${r.record_type}|${r.record_value}|${r.updated_at}`}>
                <TableCell>
                  <span className="inline-flex rounded border border-border bg-muted/40 px-1.5 font-mono text-[11px] font-medium">
                    {r.record_type}
                  </span>
                </TableCell>
                <TableCell className="font-mono text-[13px] break-all whitespace-normal">{r.record_value}</TableCell>
                <TableCell className="text-right font-mono text-xs tabular-nums text-muted-foreground">
                  {r.ttl === null ? "—" : formatInteger(r.ttl)}
                </TableCell>
                <TableCell>
                  <TimestampDisplay value={r.updated_at} />
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      </div>
    );
  }

  return (
    <Panel
      id="dns-history"
      title="DNS history"
      description={
        records && records.length > 0
          ? `${formatInteger(records.length)} ${records.length === 1 ? "record" : "records"}, newest first`
          : "All stored record versions, newest first"
      }
      flush
    >
      {body}
    </Panel>
  );
}
