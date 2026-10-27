import { FileWarningIcon } from "lucide-react";
import { useMemo, useState } from "react";

import { CopyButton } from "@/components/common/CopyButton";
import { Button } from "@/components/ui/button";
import { type BodyKind, type JsonToken, tokenizeJson } from "@/lib/body";
import { formatBytes } from "@/lib/formatters";
import { cn } from "@/lib/utils";

/** Rendering more than this at once makes the page sluggish. */
const PREVIEW_CHARS = 100_000;

const TOKEN_CLASS: Record<JsonToken["type"], string> = {
  key: "text-syntax-key",
  string: "text-syntax-string",
  number: "text-syntax-number",
  literal: "text-syntax-literal",
  punct: "text-syntax-punct",
  space: "",
};

const KIND_LABEL: Record<BodyKind, string> = {
  empty: "Empty",
  json: "JSON",
  html: "HTML",
  xml: "XML",
  text: "Text",
  binary: "Binary",
};

interface CodeViewerProps {
  /** Text to display; rendered as text, never as markup. */
  content: string;
  kind?: BodyKind;
  /** Value placed on the clipboard; defaults to `content`. */
  copyValue?: string;
  copyLabel: string;
  title?: string;
  className?: string;
}

export function CodeViewer({ content, kind = "text", copyValue, copyLabel, title, className }: CodeViewerProps) {
  const [expanded, setExpanded] = useState(false);
  const [showBinary, setShowBinary] = useState(false);
  const truncated = !expanded && content.length > PREVIEW_CHARS;
  const visible = truncated ? content.slice(0, PREVIEW_CHARS) : content;
  const tokens = useMemo(() => (kind === "json" ? tokenizeJson(visible) : null), [kind, visible]);
  const hidden = kind === "binary" && !showBinary;

  return (
    <div className={cn("overflow-hidden rounded-lg border border-border bg-background/70", className)}>
      <div className="flex h-9 items-center justify-between gap-2 border-b border-border bg-muted/30 px-3">
        <div className="flex min-w-0 items-center gap-2 text-xs text-muted-foreground">
          {title && <span className="truncate font-medium text-foreground">{title}</span>}
          <span className="rounded border border-border px-1.5 py-px font-mono text-[11px]">{KIND_LABEL[kind]}</span>
          {/* Size of what was captured, not of the pretty-printed view. */}
          <span className="tabular-nums">{formatBytes(new TextEncoder().encode(copyValue ?? content).length)}</span>
        </div>
        <CopyButton value={copyValue ?? content} label={copyLabel} />
      </div>

      {hidden ? (
        <div className="flex flex-col items-center gap-2 px-4 py-8 text-center text-sm text-muted-foreground">
          <FileWarningIcon className="size-5" aria-hidden="true" />
          <p>Binary content. Bytes that are not valid UTF-8 were replaced with U+FFFD when it was stored.</p>
          <Button variant="outline" size="sm" onClick={() => setShowBinary(true)}>
            Show as text
          </Button>
        </div>
      ) : (
        <>
          <pre
            tabIndex={0}
            className="max-h-[32rem] overflow-auto p-3 font-mono text-xs leading-relaxed break-words whitespace-pre-wrap focus-visible:ring-2 focus-visible:ring-ring/50 focus-visible:outline-none"
          >
            <code>
              {tokens
                ? tokens.map((t, i) => (
                    <span key={i} className={TOKEN_CLASS[t.type]}>
                      {t.text}
                    </span>
                  ))
                : visible}
            </code>
          </pre>
          {truncated && (
            <div className="flex items-center justify-between gap-2 border-t border-border px-3 py-2 text-xs text-muted-foreground">
              <span>Showing the first {formatBytes(PREVIEW_CHARS)} of {formatBytes(content.length)}.</span>
              <Button variant="ghost" size="sm" onClick={() => setExpanded(true)}>
                Show all
              </Button>
            </div>
          )}
        </>
      )}
    </div>
  );
}
