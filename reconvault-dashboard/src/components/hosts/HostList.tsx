import { ServerIcon } from "lucide-react";
import { useState } from "react";

import { EmptyState } from "@/components/common/EmptyState";
import { ErrorState } from "@/components/common/ErrorState";
import { HostItem } from "@/components/hosts/HostItem";
import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";
import { useDomains } from "@/features/hosts/api";

const COLLAPSED_COUNT = 12;

export function HostList() {
  const { data, error, isPending, refetch } = useDomains();
  const [showAll, setShowAll] = useState(false);

  if (isPending) {
    return (
      <div role="status" aria-label="Loading hosts" className="grid grid-cols-1 gap-2 sm:grid-cols-2 lg:grid-cols-3 2xl:grid-cols-4">
        {Array.from({ length: 6 }, (_, i) => (
          <Skeleton key={i} className="h-[62px] rounded-lg" />
        ))}
      </div>
    );
  }
  if (error) return <ErrorState error={error} title="Could not load hosts" onRetry={() => void refetch()} />;
  if (data.data.length === 0) {
    return (
      <EmptyState
        icon={ServerIcon}
        title="No hosts scanned yet"
        description="Hosts appear here after the first request to them is recorded."
      />
    );
  }

  const domains = showAll ? data.data : data.data.slice(0, COLLAPSED_COUNT);
  const hidden = data.data.length - domains.length;
  return (
    <div className="grid gap-3">
      <ul className="grid grid-cols-1 gap-2 sm:grid-cols-2 lg:grid-cols-3 2xl:grid-cols-4">
        {domains.map((d) => (
          <HostItem key={d.hostname} domain={d} />
        ))}
      </ul>
      {(hidden > 0 || showAll) && data.data.length > COLLAPSED_COUNT && (
        <Button variant="ghost" size="sm" className="justify-self-start text-muted-foreground" onClick={() => setShowAll((v) => !v)}>
          {showAll ? "Show fewer" : `Show all ${data.data.length} hosts`}
        </Button>
      )}
    </div>
  );
}
