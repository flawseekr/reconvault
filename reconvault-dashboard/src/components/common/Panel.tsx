import { type ReactNode, useId } from "react";

import { cn } from "@/lib/utils";

interface PanelProps {
  title: ReactNode;
  description?: ReactNode;
  actions?: ReactNode;
  children: ReactNode;
  /** Removes body padding (tables render edge to edge). */
  flush?: boolean;
  className?: string;
  id?: string;
}

export function Panel({ title, description, actions, children, flush, className, id }: PanelProps) {
  const headingId = useId();
  return (
    <section id={id} aria-labelledby={headingId} className={cn("overflow-hidden rounded-xl border border-border bg-card", className)}>
      <div className="flex flex-wrap items-center justify-between gap-2 border-b border-border px-4 py-3">
        <div className="min-w-0">
          <h2 id={headingId} className="text-sm font-semibold">
            {title}
          </h2>
          {description && <p className="mt-0.5 text-xs text-muted-foreground">{description}</p>}
        </div>
        {actions && <div className="flex items-center gap-2">{actions}</div>}
      </div>
      <div className={cn(!flush && "p-4")}>{children}</div>
    </section>
  );
}
