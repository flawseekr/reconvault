import { AlertTriangleIcon, RotateCwIcon } from "lucide-react";

import { Button } from "@/components/ui/button";
import { errorMessage } from "@/lib/api-client";
import { cn } from "@/lib/utils";

interface ErrorStateProps {
  error: unknown;
  title?: string;
  onRetry?: () => void;
  className?: string;
}

export function ErrorState({ error, title = "Request failed", onRetry, className }: ErrorStateProps) {
  return (
    <div role="alert" className={cn("flex flex-col items-center justify-center gap-2 px-6 py-12 text-center", className)}>
      <div className="mb-1 flex size-10 items-center justify-center rounded-lg border border-status-5xx/30 bg-status-5xx/10">
        <AlertTriangleIcon className="size-5 text-status-5xx" aria-hidden="true" />
      </div>
      <p className="text-sm font-medium">{title}</p>
      <p className="max-w-md text-sm break-words text-muted-foreground">{errorMessage(error)}</p>
      {onRetry && (
        <Button variant="outline" size="sm" className="mt-2" onClick={onRetry}>
          <RotateCwIcon /> Try again
        </Button>
      )}
    </div>
  );
}
