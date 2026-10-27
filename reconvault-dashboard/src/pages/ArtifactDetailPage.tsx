import { FileQuestionIcon, InfoIcon } from "lucide-react";
import type { ReactNode } from "react";
import { Link, useParams } from "react-router";

import { BodySize } from "@/components/artifacts/BodySize";
import { HeadersTable } from "@/components/artifacts/HeadersTable";
import { MethodBadge } from "@/components/artifacts/MethodBadge";
import { StatusBadge } from "@/components/artifacts/StatusBadge";
import { CodeViewer } from "@/components/common/CodeViewer";
import { CopyButton } from "@/components/common/CopyButton";
import { EmptyState } from "@/components/common/EmptyState";
import { ErrorState } from "@/components/common/ErrorState";
import { BlockSkeleton } from "@/components/common/LoadingSkeleton";
import { Panel } from "@/components/common/Panel";
import { PageHeader } from "@/components/layout/PageHeader";
import { Button } from "@/components/ui/button";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { useArtifact } from "@/features/artifacts/api";
import { ApiError } from "@/lib/api-client";
import { inspectBody } from "@/lib/body";
import { formatDateTime, formatExactUtc } from "@/lib/formatters";
import { headersFromRecord, headerValue, parseRawResponse } from "@/lib/http-message";
import { hostPath, routes } from "@/lib/routes";
import { isUuid } from "@/lib/validation";
import type { Artifact } from "@/types/api";

export function ArtifactDetailPage() {
  const { id } = useParams();
  const valid = isUuid(id);
  const query = useArtifact(valid ? id.toLowerCase() : undefined);
  const home = { label: "Home", to: routes.home };

  if (!valid) {
    return (
      <>
        <PageHeader title="Invalid artifact ID" breadcrumbs={[home, { label: "Artifact" }]} />
        <NotFound title="This is not a valid artifact ID" description="Artifact IDs are UUIDs such as 550e8400-e29b-41d4-a716-446655440000." />
      </>
    );
  }
  if (query.isPending) {
    return (
      <>
        <PageHeader title="Loading artifact…" breadcrumbs={[home, { label: id }]} />
        <Panel title="Overview">
          <BlockSkeleton lines={7} />
        </Panel>
      </>
    );
  }
  if (query.error) {
    const notFound = query.error instanceof ApiError && query.error.isNotFound;
    return (
      <>
        <PageHeader title={notFound ? "Artifact not found" : "Artifact"} breadcrumbs={[home, { label: id }]} />
        {notFound ? (
          <NotFound title="No artifact with this ID" description="It may have been deleted, or the link is incorrect." />
        ) : (
          <ErrorState error={query.error} title="Could not load the artifact" onRetry={() => void query.refetch()} />
        )}
      </>
    );
  }
  return <ArtifactDetail artifact={query.data} />;
}

function NotFound({ title, description }: { title: string; description: string }) {
  return (
    <EmptyState
      icon={FileQuestionIcon}
      title={title}
      description={description}
      action={
        <Button asChild variant="outline" size="sm">
          <Link to={routes.home}>Back to Home</Link>
        </Button>
      }
    />
  );
}

function ArtifactDetail({ artifact: a }: { artifact: Artifact }) {
  const response = a.response === null ? null : parseRawResponse(a.response);
  const requestHeaders = headersFromRecord(a.request_headers);
  const responseBody = response ? inspectBody(response.body, a.response_content_type) : null;
  const requestBody = inspectBody(a.request_body, headerValue(requestHeaders, "Content-Type"));

  return (
    <>
      <PageHeader
        mono
        breadcrumbs={[{ label: "Home", to: routes.home }, { label: a.hostname, to: hostPath(a.hostname) }, { label: a.id }]}
        title={
          <span className="flex min-w-0 items-start gap-2.5">
            <MethodBadge method={a.http_method} className="mt-1" />
            <span className="min-w-0 break-all">{a.full_url}</span>
          </span>
        }
        actions={<CopyButton value={a.full_url} label="Copy URL" />}
      />

      <div className="grid gap-6">
        <Panel title="Overview">
          <dl className="grid grid-cols-1 gap-x-8 gap-y-4 sm:grid-cols-2 xl:grid-cols-4">
            <Field label="Status">
              <StatusBadge code={a.response_status_code} />
            </Field>
            <Field label="Method">
              <span className="font-mono">{a.http_method}</span>
            </Field>
            <Field label="Content type">
              <Mono value={a.response_content_type} />
            </Field>
            <Field label="Body size">
              <BodySize bytes={a.response_body_size_bytes} />
              {a.response_body_size_bytes !== null && a.response_body_size_bytes >= 1024 && (
                <span className="ml-2 font-mono text-xs text-muted-foreground">{a.response_body_size_bytes.toLocaleString("en-US")} B</span>
              )}
            </Field>
            <Field label="Hostname">
              <Link to={hostPath(a.hostname)} className="font-mono break-all text-primary underline-offset-2 hover:underline">
                {a.hostname}
              </Link>
            </Field>
            <Field label="Endpoint path">
              <Mono value={a.endpoint_path} />
            </Field>
            <Field label="Requested at">
              <time dateTime={a.requested_at} className="font-mono text-xs">
                {formatDateTime(a.requested_at)}
              </time>
              <span className="block font-mono text-[11px] text-muted-foreground">{formatExactUtc(a.requested_at)}</span>
            </Field>
            <Field label="Artifact ID">
              <span className="flex items-center gap-1">
                <span className="min-w-0 font-mono text-xs break-all">{a.id}</span>
                <CopyButton value={a.id} label="Copy artifact ID" />
              </span>
            </Field>
          </dl>
        </Panel>

        <Tabs defaultValue="response">
          <TabsList aria-label="Exchange details">
            <TabsTrigger value="response">Response</TabsTrigger>
            <TabsTrigger value="request">Request</TabsTrigger>
            <TabsTrigger value="raw">Raw response</TabsTrigger>
          </TabsList>

          <TabsContent value="response" className="grid gap-4">
            {!response ? (
              <Notice>No response was recorded: the request failed before a response arrived.</Notice>
            ) : (
              <>
                {response.statusLine && (
                  <div className="rounded-lg border border-border bg-background/70 px-3 py-2 font-mono text-xs">{response.statusLine}</div>
                )}
                {response.headers ? (
                  <HeadersTable headers={response.headers} title="Response headers" />
                ) : (
                  <Notice>Response headers are not available for this artifact: it was recorded before headers were stored.</Notice>
                )}
                {responseBody && responseBody.kind !== "empty" ? (
                  <CodeViewer title="Response body" content={responseBody.display} copyValue={response.body} kind={responseBody.kind} copyLabel="Copy response body" />
                ) : (
                  <Notice>The response body is empty.</Notice>
                )}
              </>
            )}
          </TabsContent>

          <TabsContent value="request" className="grid gap-4">
            {requestHeaders ? (
              <HeadersTable headers={requestHeaders} title="Request headers" />
            ) : (
              <Notice>No request headers were recorded.</Notice>
            )}
            {requestBody.kind !== "empty" ? (
              <CodeViewer title="Request body" content={requestBody.display} copyValue={a.request_body ?? ""} kind={requestBody.kind} copyLabel="Copy request body" />
            ) : (
              <Notice>{a.request_body === null ? "No request body was sent." : "The request body is empty."}</Notice>
            )}
          </TabsContent>

          <TabsContent value="raw">
            {a.response !== null ? (
              <CodeViewer title="Raw response" content={a.response} kind="text" copyLabel="Copy raw response" />
            ) : (
              <Notice>No response was recorded.</Notice>
            )}
          </TabsContent>
        </Tabs>
      </div>
    </>
  );
}

function Field({ label, children }: { label: string; children: ReactNode }) {
  return (
    <div className="min-w-0">
      <dt className="mb-1 text-xs text-muted-foreground">{label}</dt>
      <dd className="min-w-0 text-sm">{children}</dd>
    </div>
  );
}

function Mono({ value }: { value: string | null }) {
  return value ? <span className="font-mono text-xs break-all">{value}</span> : <span className="text-muted-foreground">—</span>;
}

function Notice({ children }: { children: ReactNode }) {
  return (
    <p className="flex items-start gap-2 rounded-lg border border-border bg-muted/30 px-3 py-2.5 text-sm text-muted-foreground">
      <InfoIcon className="mt-0.5 size-4 shrink-0" aria-hidden="true" />
      {children}
    </p>
  );
}
