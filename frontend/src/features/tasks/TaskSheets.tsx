import { useState } from "react";
import { GitCompareArrows, SquareTerminal } from "lucide-react";
import type { Session } from "@/mygo";
import { Button } from "@/components/ui/button";
import { Sheet, SheetDescription, SheetHeader, SheetPopup, SheetTitle, SheetTrigger } from "@/components/ui/sheet";
import { SessionDiff } from "@/features/agents/shared";
import { isRemoteProcess, sessionTitle } from "@/features/agents/format";
import { TerminalPane } from "@/features/terminals/TerminalPane";
import { PublishPanel } from "./PublishPanel";

export function TaskSheets({ session }: { session: Session }) {
  const [terminalOpen, setTerminalOpen] = useState(false);
  if (isRemoteProcess(session)) return null;
  return <>
    <Sheet>
      <SheetTrigger render={<Button size="xs" variant="ghost" />}><GitCompareArrows data-icon="inline-start" />Changes</SheetTrigger>
      <SheetPopup side="right" className="w-full max-w-4xl">
        <SheetHeader className="shrink-0 pr-12">
          <SheetTitle>Changes</SheetTitle>
          <SheetDescription>{sessionTitle(session)}{session.branch ? ` · ${session.branch}` : ""}</SheetDescription>
        </SheetHeader>
        <div className="flex min-h-0 flex-1 flex-col overflow-auto">
          <PublishPanel session={session} />
          <div className="flex min-h-64 flex-1 p-3"><SessionDiff session={session} /></div>
        </div>
      </SheetPopup>
    </Sheet>
    {session.mode === "terminal" && <Sheet open={terminalOpen} onOpenChange={setTerminalOpen}>
      <SheetTrigger render={<Button size="xs" variant="ghost" />}><SquareTerminal data-icon="inline-start" />Terminal</SheetTrigger>
      <SheetPopup side="right" className="w-full max-w-4xl">
        <SheetHeader className="shrink-0 pr-12"><SheetTitle>Terminal</SheetTitle><SheetDescription>{sessionTitle(session)}</SheetDescription></SheetHeader>
        <div className="flex min-h-0 flex-1 p-3">
          {session.terminalId ? <TerminalPane id={session.terminalId} active={terminalOpen} onFocus={() => {}} fontSize={13} /> : <p role="status" className="m-auto text-muted-foreground">This session has no attached terminal.</p>}
        </div>
      </SheetPopup>
    </Sheet>}
  </>;
}
