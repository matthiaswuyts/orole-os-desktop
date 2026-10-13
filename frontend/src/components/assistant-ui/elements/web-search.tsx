import { SearchIcon } from "lucide-react";
import type { ComponentProps } from "react";
import { cn } from "@/lib/utils";
import { surfaces } from "./surfaces";

export interface WebSearchResult { title: string; domain: string }

/**
 * assistant-ui's web search (https://www.assistant-ui.com/elements/web-search): the query in a pill, a status line and the
 * results as domain-lettered rows. Fully controlled; `cycle` is mixed into each row's key to replay the entrance.
 */
export function WebSearch({ query, results, visibleResults, searching, cycle, className, ...props }: Omit<ComponentProps<"div">, "results"> & {
  query: string;
  results: readonly WebSearchResult[];
  visibleResults: number;
  searching: boolean;
  cycle: number;
}) {
  const count = Math.max(0, Math.min(results.length, Number.isFinite(visibleResults) ? Math.floor(visibleResults) : 0));
  const shown = results.slice(0, count);
  return (
    <div data-slot="web-search" className={cn("flex min-w-0 flex-col gap-2", className)} {...props}>
      {query && <span className={cn(surfaces.field, "inline-flex max-w-full items-center gap-1.5 self-start px-2.5 py-1 text-xs")}>
        <SearchIcon aria-hidden className="size-3 shrink-0 text-muted-foreground" />
        <span className="truncate">{query}</span>
      </span>}
      <div role="status" className="text-muted-foreground text-xs">
        {searching ? <span className="cv-shimmer font-medium">Searching</span> : `Read ${results.length} ${results.length === 1 ? "source" : "sources"}`}
      </div>
      {shown.length > 0 && <ul aria-label="Search results" className="m-0 flex max-h-56 list-none flex-col gap-1 overflow-y-auto p-0">
        {shown.map((result, index) => (
          <li key={`${cycle}:${index}:${result.domain}`} className="flex min-w-0 items-center gap-2 text-xs animate-in fade-in-0 motion-reduce:animate-none">
            <span aria-hidden className="grid size-5 shrink-0 place-items-center rounded-full bg-muted font-medium text-[10px] uppercase">{result.domain.replace(/^www\./, "").charAt(0)}</span>
            <span className="min-w-0 flex-1 truncate">{result.title}</span>
            <span className={cn(surfaces.mono, "shrink-0")}>{result.domain}</span>
          </li>
        ))}
      </ul>}
    </div>
  );
}
