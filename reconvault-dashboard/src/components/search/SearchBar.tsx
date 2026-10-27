import { SearchIcon, XIcon } from "lucide-react";
import { type FormEvent, useId, useState } from "react";

import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { cn } from "@/lib/utils";
import { validateSearchQuery } from "@/lib/validation";

interface SearchBarProps {
  defaultValue?: string;
  /** Called with a validated query on submit (Enter or button). */
  onSearch: (query: string) => void;
  size?: "default" | "lg";
  autoFocus?: boolean;
  className?: string;
}

/** Submit-only search input: no request is made while typing. */
export function SearchBar({ defaultValue = "", onSearch, size = "default", autoFocus, className }: SearchBarProps) {
  const [value, setValue] = useState(defaultValue);
  const [error, setError] = useState<string | null>(null);
  const errorId = useId();

  function submit(event: FormEvent) {
    event.preventDefault();
    const problem = validateSearchQuery(value);
    setError(problem);
    if (!problem) onSearch(value);
  }

  return (
    <form role="search" onSubmit={submit} className={cn("grid gap-1.5", className)} noValidate>
      <div className="flex gap-2">
        <div className="relative flex-1">
          <SearchIcon
            className={cn("pointer-events-none absolute top-1/2 left-3 -translate-y-1/2 text-muted-foreground", size === "lg" ? "size-4.5" : "size-4")}
            aria-hidden="true"
          />
          <Input
            type="search"
            value={value}
            onChange={(e) => {
              setValue(e.target.value);
              if (error) setError(null);
            }}
            placeholder="Search URLs, headers, bodies, tokens…"
            aria-label="Search artifacts"
            aria-invalid={error ? true : undefined}
            aria-describedby={error ? errorId : undefined}
            autoFocus={autoFocus}
            maxLength={1024}
            className={cn("pr-9 pl-9 font-mono [&::-webkit-search-cancel-button]:hidden", size === "lg" && "h-11 text-[15px]")}
          />
          {value && (
            <button
              type="button"
              onClick={() => {
                setValue("");
                setError(null);
              }}
              className="absolute top-1/2 right-2 -translate-y-1/2 rounded p-1 text-muted-foreground hover:text-foreground focus-visible:ring-2 focus-visible:ring-ring/60 focus-visible:outline-none"
              aria-label="Clear search"
            >
              <XIcon className="size-3.5" />
            </button>
          )}
        </div>
        <Button type="submit" size={size === "lg" ? "lg" : "default"} className={cn(size === "lg" && "h-11")}>
          Search
        </Button>
      </div>
      {error && (
        <p id={errorId} className="text-xs text-destructive">
          {error}
        </p>
      )}
    </form>
  );
}
