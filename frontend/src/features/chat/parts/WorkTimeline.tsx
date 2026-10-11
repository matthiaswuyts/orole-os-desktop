import { useState } from "react";
import { ResearchReport } from "@/components/assistant-ui/elements/research-report";
import { ToolTimeline } from "@/components/assistant-ui/elements/tool-timeline";
import { AgentHandoff } from "@/components/assistant-ui/elements/agent-handoff";
import { WebSearch } from "@/components/assistant-ui/elements/web-search";
import { BotAvatar } from "@/components/shell/BotAvatar";
import { Button } from "@/components/ui/button";
import { cn } from "@/lib/utils";
import type { Source } from "./format";
import { asResult, type Work } from "./work";

/** The few bot fields a handoff needs to draw a bot. */
export interface BotRef { id: string; name: string; avatar: string }

/**
 * Everything the assistant did behind one collapsed line: the research outline, the web search with the sources it read,
 * and the plain tool steps. Closed by default; the reply itself stays outside it.
 */
export function WorkTimeline({ work, sources, botName, botAvatar, bots, onOpenBot }: { work: Work; sources: readonly Source[]; botName: string; botAvatar: string; bots: readonly BotRef[]; onOpenBot?: (botId: string) => void }) {
  const [open, setOpen] = useState(false);
  const { steps, research, query, searching, count, streaming, handoffs } = work;
  const results = sources.map(asResult);
  const label = research && research.sourcesRead > 0 ? `Researched ${research.sourcesRead} sources` : `${count} ${count === 1 ? "step" : "steps"}`;
  return (
    <ToolTimeline steps={steps} visibleSteps={steps.length} streaming={streaming} open={open} onOpenChange={setOpen}
      restingLabel={label} activeLabel={research ? "Researching" : "Working"} stats={[]}>
      {handoffs.map((handoff) => {
        const callee = bots.find((b) => b.id === handoff.botId);
        return <div key={handoff.id} className="flex min-w-0 flex-col items-start gap-1.5">
          <AgentHandoff from={botName} to={handoff.to} fromAvatar={<BotAvatar name={botName} avatar={botAvatar} className="size-4 text-[9px]" />}
            toAvatar={<BotAvatar name={handoff.to} avatar={callee?.avatar ?? ""} className="size-4 text-[9px]" />}
            reason={`Handoff by ${botName}`} carried={[]} settled={handoff.state !== "running"} />
          {handoff.request && <p className="selectable line-clamp-4 min-w-0 whitespace-pre-wrap break-words border-s-2 ps-3 text-muted-foreground"><span className="font-medium text-foreground">{botName} asked: </span>{handoff.request}</p>}
          {handoff.outcome && <p className={cn("selectable line-clamp-4 min-w-0 whitespace-pre-wrap break-words border-s-2 ps-3 text-muted-foreground", handoff.state === "error" && "border-destructive/50")}><span className={cn("font-medium", handoff.state === "error" ? "text-destructive-foreground" : "text-foreground")}>{handoff.state === "error" ? `${handoff.to} could not answer: ` : `${handoff.to} answered: `}</span>{handoff.outcome}</p>}
          {handoff.botId && onOpenBot && <Button type="button" variant="outline" size="xs" onClick={() => onOpenBot(handoff.botId!)}>Open {handoff.to}'s chat</Button>}
        </div>;
      })}
      {research && research.sections.length > 0 && <ResearchReport title={research.title} sections={research.sections} sourcesRead={research.sourcesRead} />}
      {(results.length > 0 || searching) && <WebSearch query={query} results={results} visibleResults={results.length} searching={searching} cycle={0} />}
    </ToolTimeline>
  );
}
