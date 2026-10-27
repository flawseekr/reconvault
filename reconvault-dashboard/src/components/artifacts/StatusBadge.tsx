import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import { type StatusClass, statusClass } from "@/lib/formatters";
import { cn } from "@/lib/utils";

const STATUS_CLASS: Record<StatusClass, string> = {
  "1xx": "text-status-none bg-status-none/10 border-status-none/25",
  "2xx": "text-status-2xx bg-status-2xx/10 border-status-2xx/25",
  "3xx": "text-status-3xx bg-status-3xx/10 border-status-3xx/25",
  "4xx": "text-status-4xx bg-status-4xx/10 border-status-4xx/25",
  "5xx": "text-status-5xx bg-status-5xx/10 border-status-5xx/25",
  none: "",
};

export function StatusBadge({ code, className }: { code: number | null; className?: string }) {
  const cls = statusClass(code);
  if (code === null) {
    return (
      <Tooltip>
        <TooltipTrigger asChild>
          <span tabIndex={0} className="font-mono text-xs text-muted-foreground" aria-label="No response">
            —
          </span>
        </TooltipTrigger>
        <TooltipContent>No response: the request failed before a response arrived.</TooltipContent>
      </Tooltip>
    );
  }
  return (
    <span
      data-status-class={cls}
      className={cn(
        "inline-flex h-5 items-center rounded border px-1.5 font-mono text-[11px] font-semibold tabular-nums",
        STATUS_CLASS[cls],
        className,
      )}
    >
      {code}
    </span>
  );
}
