import { CopyButton } from "@/components/common/CopyButton";
import { formatHeaders, type HeaderEntry } from "@/lib/http-message";

interface HeadersTableProps {
  headers: HeaderEntry[];
  title: string;
}

export function HeadersTable({ headers, title }: HeadersTableProps) {
  return (
    <div className="overflow-hidden rounded-lg border border-border bg-background/70">
      <div className="flex h-9 items-center justify-between gap-2 border-b border-border bg-muted/30 px-3">
        <span className="text-xs font-medium">
          {title} <span className="font-normal text-muted-foreground">({headers.length})</span>
        </span>
        <CopyButton value={formatHeaders(headers)} label={`Copy ${title.toLowerCase()}`} />
      </div>
      {headers.length === 0 ? (
        <p className="px-3 py-3 text-xs text-muted-foreground">No headers.</p>
      ) : (
        <dl className="grid grid-cols-1 font-mono text-xs sm:grid-cols-[minmax(9rem,max-content)_1fr]">
          {headers.map((h, i) => (
            <div key={`${h.name}-${i}`} className="contents">
              <dt className="border-b border-border/60 px-3 pt-1.5 text-syntax-key sm:py-1.5">{h.name}</dt>
              <dd className="border-b border-border/60 px-3 pb-1.5 break-all text-foreground/90 sm:py-1.5">
                {h.value || <span className="text-muted-foreground">(empty)</span>}
              </dd>
            </div>
          ))}
        </dl>
      )}
    </div>
  );
}
