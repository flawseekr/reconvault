import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import { formatBytes, formatInteger } from "@/lib/formatters";

export function BodySize({ bytes }: { bytes: number | null }) {
  if (bytes === null) return <span className="text-muted-foreground">—</span>;
  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <span tabIndex={0} className="font-mono text-xs tabular-nums">
          {formatBytes(bytes)}
        </span>
      </TooltipTrigger>
      <TooltipContent>{formatInteger(bytes)} bytes</TooltipContent>
    </Tooltip>
  );
}
