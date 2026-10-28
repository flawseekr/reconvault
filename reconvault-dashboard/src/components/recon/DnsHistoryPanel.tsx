import { NetworkIcon } from "lucide-react";
import type { ReactNode } from "react";

import { EmptyState } from "@/components/common/EmptyState";
import { ErrorState } from "@/components/common/ErrorState";
import { TableSkeleton } from "@/components/common/LoadingSkeleton";
import { Panel } from "@/components/common/Panel";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { useDnsHistory } from "@/features/recon/api";
import { formatInteger } from "@/lib/formatters";
import type { DnsRecord } from "@/types/api";

/**
 * Collapses the history to unique (type, value) pairs. The API returns one
 * row per scan version, so an unchanged record repeats once per scan; the
 * first occurrence (newest, in server order) is kept.
 */
function uniqueDnsRecords(records: DnsRecord[]): DnsRecord[] {
  const seen = new Set<string>();
  return records.filter((r) => {
    const key = `${r.record_type}\u0000${r.record_value}`;
    if (seen.has(key)) return false;
    seen.add(key);
    return true;
  });
}

/** Every DNS record type and value seen for a hostname, newest first. */
export function DnsHistoryPanel({ hostname }: { hostname: string }) {
  const { data, error, isPending, refetch } = useDnsHistory(hostname);
  const records = data?.data ? uniqueDnsRecords(data.data) : undefined;

  let body: ReactNode;
  if (isPending) {
    body = <TableSkeleton rows={4} columns={2} />;
  } else if (error) {
    body = <ErrorState error={error} title="Failed to load DNS history" onRetry={() => void refetch()} />;
  } else if (!records || records.length === 0) {
    body = (
      <EmptyState icon={NetworkIcon} title="No DNS records" description="DNS autoscan hasn't stored any records for this hostname." />
    );
  } else {
    body = (
      // Histories are unpaginated; cap the height so the page stays short.
      <div className="max-h-[28rem] overflow-y-auto">
        <Table className="table-fixed">
          <caption className="sr-only">DNS history for {hostname}</caption>
          <colgroup>
            <col className="w-20 sm:w-24" />
            <col />
          </colgroup>
          <TableHeader className="sticky top-0 z-10 bg-card">
            <TableRow className="hover:bg-transparent">
              <TableHead>Type</TableHead>
              <TableHead>Value</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {records.map((r) => (
              <TableRow key={`${r.record_type}|${r.record_value}`}>
                <TableCell className="align-top">
                  <span className="inline-flex rounded border border-border bg-muted/40 px-1.5 font-mono text-[11px] font-medium">
                    {r.record_type}
                  </span>
                </TableCell>
                <TableCell className="align-top font-mono text-[13px] break-all whitespace-normal">{r.record_value}</TableCell>
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
          : "Record types and values, newest first"
      }
      flush
    >
      {body}
    </Panel>
  );
}
