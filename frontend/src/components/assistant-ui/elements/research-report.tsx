import { CheckIcon, Loader2Icon } from "lucide-react";
import type { ComponentProps } from "react";
import { cn } from "@/lib/utils";
import { surfaces } from "./surfaces";

export interface ReportSection { id: string; heading: string; state: "pending" | "writing" | "done"; sources: number; preview?: string }

/**
 * assistant-ui's research report (https://www.assistant-ui.com/elements/research-report): an outline that tracks pending,
 * writing and done per section, with a source count each. Only the glyph tells writing from done.
 */
export function ResearchReport({ title, sections, sourcesRead, className, ...props }: Omit<ComponentProps<"div">, "title"> & {
  title: string;
  sections: readonly ReportSection[];
  sourcesRead: number;
}) {
  const done = sections.filter((section) => section.state === "done").length;
  return (
    <div data-slot="research-report" className={cn(surfaces.paper, "flex min-w-0 flex-col text-xs", className)} {...props}>
      <div className="flex min-w-0 items-baseline justify-between gap-3 px-3 py-2">
        <span className="truncate font-medium text-sm">{title}</span>
        <span className={cn(surfaces.mono, "shrink-0")}>{done}/{sections.length} sections · {sourcesRead} sources read</span>
      </div>
      {sections.map((section, index) => (
        <div key={section.id} className={cn("flex min-w-0 flex-col gap-0.5 px-3 py-2", index > 0 && "border-t", index === 0 && "border-t")}>
          <div className="flex min-w-0 items-center gap-2">
            <span aria-hidden className="grid size-4 shrink-0 place-items-center">
              {section.state === "done" ? <CheckIcon className="size-3.5 text-success" />
                : section.state === "writing" ? <Loader2Icon className="size-3.5 animate-spin motion-reduce:animate-none" />
                  : <span className="size-1.5 rounded-full bg-muted-foreground/50" />}
            </span>
            <span className={cn("min-w-0 flex-1 truncate", section.state === "pending" && "text-muted-foreground")} title={section.heading}>{section.heading}</span>
            {section.sources > 0 && <span className={cn(surfaces.mono, "shrink-0")}>{section.sources} src</span>}
          </div>
          {section.preview && <p className="ps-6 text-muted-foreground">{section.preview}</p>}
        </div>
      ))}
    </div>
  );
}
