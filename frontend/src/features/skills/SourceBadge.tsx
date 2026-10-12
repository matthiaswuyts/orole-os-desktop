import { Globe, PenLine, Puzzle } from "lucide-react";
import { ProviderLogo } from "@/features/agents/ProviderLogo";
import { cn } from "@/lib/utils";
import { sourceInfo } from "./skills";

/**
 * Where a skill came from, as a pill with the real mark of the source: the agent's own logo for
 * skills imported from Claude Code, Codex or Oh My Pi, a globe for skills.sh, a pen for skills written
 * in Orole.
 */
export function SourceBadge({ source, className }: { source: string; className?: string }) {
  const info = sourceInfo(source);
  return <span className={cn("inline-flex max-w-full items-center gap-1.5 rounded-full border bg-muted/40 py-0.5 pr-2.5 pl-1.5 text-muted-foreground text-xs", className)}>
    <span aria-hidden className="flex size-4 shrink-0 items-center justify-center text-foreground [&_svg]:size-3.5">
      {info.kind === "harness" && info.harness && <ProviderLogo kind={info.harness} />}
      {info.kind === "hub" && <Globe />}
      {info.kind === "orole" && <PenLine />}
      {(info.kind === "builtin" || info.kind === "other") && <Puzzle />}
    </span>
    <span className="truncate">{info.label}</span>
  </span>;
}
