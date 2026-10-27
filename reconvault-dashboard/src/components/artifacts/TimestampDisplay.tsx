import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import { formatDateTime, formatExactUtc, formatRelative } from "@/lib/formatters";
import { cn } from "@/lib/utils";

interface TimestampDisplayProps {
  value: string;
  /** "absolute" shows local date-time; "relative" shows e.g. "5 minutes ago". */
  variant?: "absolute" | "relative";
  className?: string;
}

export function TimestampDisplay({ value, variant = "absolute", className }: TimestampDisplayProps) {
  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <time dateTime={value} tabIndex={0} className={cn("font-mono text-xs tabular-nums text-muted-foreground", className)}>
          {variant === "relative" ? formatRelative(value) : formatDateTime(value)}
        </time>
      </TooltipTrigger>
      <TooltipContent>
        <span className="font-mono">{formatExactUtc(value)}</span>
      </TooltipContent>
    </Tooltip>
  );
}
