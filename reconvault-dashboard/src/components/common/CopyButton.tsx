import { CheckIcon, CopyIcon } from "lucide-react";
import { useEffect, useState } from "react";

import { Button } from "@/components/ui/button";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import { copyText } from "@/lib/clipboard";
import { cn } from "@/lib/utils";

interface CopyButtonProps {
  value: string;
  label: string;
  className?: string;
}

export function CopyButton({ value, label, className }: CopyButtonProps) {
  const [status, setStatus] = useState<"idle" | "copied" | "failed">("idle");

  useEffect(() => {
    if (status === "idle") return;
    const t = setTimeout(() => setStatus("idle"), 1500);
    return () => clearTimeout(t);
  }, [status]);

  async function copy() {
    setStatus((await copyText(value)) ? "copied" : "failed");
  }

  const tip = status === "copied" ? "Copied" : status === "failed" ? "Copy failed" : label;
  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <Button
          type="button"
          variant="ghost"
          size="icon-sm"
          onClick={copy}
          aria-label={label}
          className={cn("text-muted-foreground hover:text-foreground", className)}
        >
          {status === "copied" ? <CheckIcon className="text-status-2xx" /> : <CopyIcon />}
        </Button>
      </TooltipTrigger>
      <TooltipContent>{tip}</TooltipContent>
      <span className="sr-only" aria-live="polite">
        {status === "copied" ? "Copied to clipboard" : status === "failed" ? "Copy failed" : ""}
      </span>
    </Tooltip>
  );
}
