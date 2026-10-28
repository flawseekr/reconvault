import { zodResolver } from "@hookform/resolvers/zod";
import { ArrowRightIcon, Loader2Icon, SendIcon } from "lucide-react";
import { useForm } from "react-hook-form";
import { Link } from "react-router";
import { z } from "zod";

import { MethodBadge } from "@/components/artifacts/MethodBadge";
import { StatusBadge } from "@/components/artifacts/StatusBadge";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { useExecuteRequest } from "@/features/artifacts/api";
import { ApiError, errorMessage } from "@/lib/api-client";
import { formatBytes } from "@/lib/formatters";
import { artifactPath } from "@/lib/routes";
import { targetUrlSchema } from "@/lib/validation";

const schema = z.object({ url: targetUrlSchema });
type FormValues = z.infer<typeof schema>;

/** Records a GET request via POST /api/v1/http/request. */
export function RequestBar() {
  const execute = useExecuteRequest();
  const {
    register,
    handleSubmit,
    formState: { errors },
  } = useForm<FormValues>({ resolver: zodResolver(schema), defaultValues: { url: "" } });

  const onSubmit = (values: FormValues) => execute.mutate({ method: "GET", url: values.url });
  const failedArtifactId = execute.error instanceof ApiError ? execute.error.artifactId : undefined;

  return (
    <div className="grid gap-2">
      <form onSubmit={handleSubmit(onSubmit)} className="flex gap-2" noValidate>
        <div className="relative flex-1">
          <span className="pointer-events-none absolute top-1/2 left-2 -translate-y-1/2">
            <MethodBadge method="GET" />
          </span>
          <Input
            {...register("url")}
            type="url"
            inputMode="url"
            placeholder="https://example.com/path"
            aria-label="URL to probe"
            aria-invalid={errors.url ? true : undefined}
            aria-describedby="request-bar-status"
            autoComplete="off"
            spellCheck={false}
            className="pl-[4.5rem] font-mono"
          />
        </div>
        <Button type="submit" disabled={execute.isPending}>
          {execute.isPending ? <Loader2Icon className="animate-spin" /> : <SendIcon />}
          <span className="hidden sm:inline">{execute.isPending ? "Sending…" : "Send"}</span>
          <span className="sr-only sm:hidden">Send request</span>
        </Button>
      </form>

      <div id="request-bar-status" aria-live="polite" className="min-h-5 text-xs">
        {errors.url ? (
          <p className="text-destructive">{errors.url.message}</p>
        ) : execute.isError ? (
          <p className="text-destructive">
            {errorMessage(execute.error)}
            {failedArtifactId && (
              <>
                {" "}
                The failed attempt was recorded.{" "}
                <Link to={artifactPath(failedArtifactId)} className="underline underline-offset-2 hover:text-foreground">
                  View artifact
                </Link>
              </>
            )}
          </p>
        ) : execute.isSuccess ? (
          <p className="flex flex-wrap items-center gap-2 text-muted-foreground">
            <span>Recorded</span>
            <StatusBadge code={execute.data.response_status_code} />
            {execute.data.response_body_size_bytes !== null && (
              <span className="font-mono">{formatBytes(execute.data.response_body_size_bytes)}</span>
            )}
            {execute.data.response_body_truncated && <span className="text-status-4xx">(body truncated)</span>}
            <Link
              to={artifactPath(execute.data.id)}
              className="inline-flex items-center gap-1 text-primary underline-offset-2 hover:underline"
            >
              View artifact <ArrowRightIcon className="size-3" />
            </Link>
          </p>
        ) : null}
      </div>
    </div>
  );
}
