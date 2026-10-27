import { GlobeIcon } from "lucide-react";
import { Link } from "react-router";

import { TimestampDisplay } from "@/components/artifacts/TimestampDisplay";
import { formatInteger } from "@/lib/formatters";
import { hostPath } from "@/lib/routes";
import type { Domain } from "@/types/api";

export function HostItem({ domain }: { domain: Domain }) {
  return (
    <li className="min-w-0">
      <Link
        to={hostPath(domain.hostname)}
        className="group flex h-full min-w-0 flex-col gap-1.5 rounded-lg border border-border bg-card px-3 py-2.5 transition-colors outline-none hover:border-primary/40 hover:bg-accent/40 focus-visible:ring-2 focus-visible:ring-ring/60"
      >
        <div className="flex min-w-0 items-center gap-2">
          <GlobeIcon className="size-3.5 shrink-0 text-muted-foreground group-hover:text-primary" aria-hidden="true" />
          <span className="min-w-0 truncate font-mono text-[13px] font-medium" title={domain.hostname}>
            {domain.hostname}
          </span>
        </div>
        <div className="flex items-center justify-between gap-2 pl-5.5">
          <span className="inline-flex items-center rounded border border-border bg-muted/40 px-1.5 font-mono text-[11px] tabular-nums text-muted-foreground">
            {formatInteger(domain.artifact_count)} {domain.artifact_count === 1 ? "artifact" : "artifacts"}
          </span>
          <TimestampDisplay value={domain.last_requested_at} variant="relative" className="text-[11px]" />
        </div>
      </Link>
    </li>
  );
}
