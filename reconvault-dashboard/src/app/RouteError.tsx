import { AlertTriangleIcon } from "lucide-react";
import { isRouteErrorResponse, Link, useRouteError } from "react-router";

import { Button } from "@/components/ui/button";

/** Shown while the first lazily loaded route chunk downloads. */
export function RouteLoading() {
  return (
    <div role="status" aria-label="Loading" className="flex min-h-dvh items-center justify-center">
      <span className="size-5 animate-spin rounded-full border-2 border-muted border-t-primary" aria-hidden="true" />
    </div>
  );
}

/** Last-resort boundary for render errors inside a route. */
export function RouteError() {
  const error = useRouteError();
  const message = isRouteErrorResponse(error)
    ? `${error.status} ${error.statusText}`
    : error instanceof Error
      ? error.message
      : "Unexpected error.";
  return (
    <div role="alert" className="mx-auto flex min-h-dvh max-w-md flex-col items-center justify-center gap-3 px-6 text-center">
      <AlertTriangleIcon className="size-6 text-status-5xx" aria-hidden="true" />
      <h1 className="text-lg font-semibold">This page failed to render</h1>
      <p className="text-sm break-words text-muted-foreground">{message}</p>
      <Button asChild variant="outline" size="sm">
        <Link to="/" reloadDocument>
          Reload Home
        </Link>
      </Button>
    </div>
  );
}
