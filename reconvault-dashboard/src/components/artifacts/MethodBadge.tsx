import { cn } from "@/lib/utils";

const METHOD_CLASS: Record<string, string> = {
  GET: "text-method-get bg-method-get/10 border-method-get/25",
  POST: "text-method-post bg-method-post/10 border-method-post/25",
  PUT: "text-method-put bg-method-put/10 border-method-put/25",
  PATCH: "text-method-patch bg-method-patch/10 border-method-patch/25",
  DELETE: "text-method-delete bg-method-delete/10 border-method-delete/25",
};

export function MethodBadge({ method, className }: { method: string; className?: string }) {
  return (
    <span
      className={cn(
        "inline-flex h-5 min-w-[3.25rem] shrink-0 items-center justify-center rounded border px-1.5 font-mono text-[11px] font-semibold tracking-wide",
        METHOD_CLASS[method] ?? "border-method-other/25 bg-method-other/10 text-method-other",
        className,
      )}
    >
      {method}
    </span>
  );
}
