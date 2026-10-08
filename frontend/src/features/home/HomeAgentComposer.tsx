import { useEffect } from "react";
import { Tabs } from "@base-ui/react/tabs";
import { ArrowUpRight, RefreshCw } from "lucide-react";
import type { Session } from "@/mygo";
import { Button } from "@/components/ui/button";
import { Frame, FramePanel } from "@/components/ui/frame";
import { PreviewCard, PreviewCardPopup, PreviewCardTrigger } from "@/components/ui/preview-card";
import { AgentLaunchForm } from "@/features/agents/AgentLaunchForm";
import { AgentGlyph, ErrorNote, SessionBadge } from "@/features/agents/shared";
import { sessionTitle } from "@/features/agents/format";
import { useAgents } from "@/stores/agents";
import { useBoxes } from "@/stores/boxes";
import { useUI } from "@/stores/ui";

const tabClass = "flex items-center gap-2 border-b-2 border-transparent px-1 py-2 text-sm text-muted-foreground outline-none data-active:border-primary data-active:text-foreground focus-visible:rounded-sm focus-visible:ring-2 focus-visible:ring-ring";

function inspectSession(session: Session) {
  useAgents.getState().setOpenId(session.id);
  useUI.getState().setView("agents");
}

export function HomeAgentComposer() {
  const { sessions, init } = useAgents();
  const liveCount = Object.values(sessions).filter((session) => session.live && session.review !== "discarded").length;
  useEffect(() => init(), [init]);
  return <Tabs.Root defaultValue="new" className="w-full min-w-0">
    <Tabs.List aria-label="Agent tasks" className="mb-3 flex justify-center gap-5">
      <Tabs.Tab value="new" className={tabClass}>New task</Tabs.Tab>
      <Tabs.Tab value="running" className={tabClass}>Running agents<span className="text-xs tabular-nums">{liveCount}</span></Tabs.Tab>
    </Tabs.List>
    <Frame className="w-full shadow-lg/5"><FramePanel className="p-0">
      <Tabs.Panel value="new" keepMounted className="outline-none [[hidden]]:hidden">
        <AgentLaunchForm onStarted={(session) => { useAgents.getState().upsert(session); inspectSession(session); }} />
      </Tabs.Panel>
      <Tabs.Panel value="running" className="outline-none [[hidden]]:hidden"><RunningAgents /></Tabs.Panel>
    </FramePanel></Frame>
  </Tabs.Root>;
}

function RunningAgents() {
  const { sessions, available, loaded, loading, offline, error, refresh } = useAgents();
  const machines = useBoxes((state) => state.machines);
  const items = Object.values(sessions).filter((session) => session.review !== "discarded")
    .sort((left, right) => Date.parse(right.updatedAt) - Date.parse(left.updatedAt));
  const live = items.filter((session) => session.live);
  const recent = items.filter((session) => !session.live).slice(0, 5);
  const rows = (list: Session[]) => list.map((session) => {
    const box = machines.find((item) => item.id === (session.machine || "local"));
    return <div key={session.id} className="flex min-w-0 items-center gap-2 py-2">
      <AgentGlyph kind={session.kind} />
      <div className="min-w-0 flex-1">
        <PreviewCard>
          <PreviewCardTrigger render={<button type="button" onClick={() => inspectSession(session)} className="block max-w-full truncate rounded-sm text-left text-sm font-medium outline-none hover:underline focus-visible:ring-2 focus-visible:ring-ring" />}>
            {sessionTitle(session)}
          </PreviewCardTrigger>
          <PreviewCardPopup align="start" className="w-80 max-w-[calc(100vw-2rem)]">
            <dl className="flex min-w-0 flex-col gap-2 text-xs">
              <div><dt className="font-medium">Box</dt><dd>{box?.name ?? "Box name unavailable"}</dd></div>
              <div><dt className="font-medium">Working directory</dt><dd className="break-all text-muted-foreground">{session.dir}</dd></div>
              <div><dt className="font-medium">Latest activity</dt><dd className="line-clamp-5 whitespace-pre-wrap break-words text-muted-foreground">{session.activity || session.lastMessage || session.error || session.status}</dd></div>
            </dl>
          </PreviewCardPopup>
        </PreviewCard>
        <p className="truncate text-xs text-muted-foreground">{session.activity || box?.name || session.dir}</p>
      </div>
      <SessionBadge session={session} />
      <Button type="button" size="icon-xs" variant="ghost" aria-label={`Inspect ${sessionTitle(session)}`} onClick={() => inspectSession(session)}><ArrowUpRight /></Button>
    </div>;
  });
  return <div className="flex min-h-52 flex-col gap-2 p-4">
    <div className="flex items-center justify-between gap-2"><h2 className="text-sm font-medium">{offline ? "Last known sessions" : "Live sessions"}</h2><Button type="button" variant="ghost" size="xs" disabled={loading || !available} onClick={() => void refresh()}><RefreshCw />Refresh</Button></div>
    <ErrorNote error={error} />
    {!available ? <p className="text-sm text-muted-foreground">Open the desktop app to connect to your agents.</p>
      : !loaded ? <p role="status" className="text-sm text-muted-foreground">Loading agent sessions…</p>
      : <div className="max-h-80 overflow-y-auto">
        {live.length ? rows(live) : <p className="py-3 text-sm text-muted-foreground">{offline ? "Reconnect the daemon to see which agents are running." : "No agents are running. Start a task in the New task tab."}</p>}
        {recent.length > 0 && <><h3 className="pt-3 text-xs font-medium text-muted-foreground">Recent sessions · open to inspect or resume</h3>{rows(recent)}</>}
      </div>}
    <Button type="button" variant="ghost" size="xs" className="mt-auto self-start" onClick={() => { useAgents.getState().setOpenId(null); useUI.getState().setView("agents"); }}>All agent sessions<ArrowUpRight /></Button>
  </div>;
}
