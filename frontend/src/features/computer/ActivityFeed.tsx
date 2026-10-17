import { useEffect, useLayoutEffect, useRef, useState } from "react";
import { CheckIcon, CircleXIcon, GlobeIcon, LoaderCircleIcon, SquareTerminalIcon, FileTextIcon } from "lucide-react";
import { Sandbox, events, type ActivityEntry } from "@/mygo";
import { cn } from "@/lib/utils";
import { errorText } from "./computerState";

/** What the bot did on its computer: loaded once, then kept current by sandbox:activity (an id arrives again when its call finishes). */
export function useActivity(botId: string) {
  const [entries, setEntries] = useState<ActivityEntry[] | null>(null);
  const [error, setError] = useState<string | null>(null);
  useEffect(() => {
    let live = true;
    setEntries(null);
    setError(null);
    // Events that arrive before the first listing are kept and merged into it.
    const early: ActivityEntry[] = [];
    let loaded = false;
    const upsert = (list: ActivityEntry[], e: ActivityEntry) => {
      const i = list.findIndex((x) => x.id === e.id);
      return i < 0 ? [...list, e] : list.map((x, j) => (j === i ? e : x));
    };
    const off = events.sandboxActivity.on((e) => {
      if (e.botId !== botId || !live) return;
      if (!loaded) early.push(e); else setEntries((list) => upsert(list ?? [], e));
    });
    Sandbox.activity(botId).then(
      (list) => {
        if (!live) return;
        loaded = true;
        setEntries(early.reduce(upsert, list ?? []));
      },
      (e: unknown) => { if (live) setError(errorText(e)); },
    );
    return () => { live = false; off(); };
  }, [botId]);
  return { entries, error };
}

const KIND_ICON = { command: SquareTerminalIcon, file: FileTextIcon, browser: GlobeIcon } as const;
const KIND_LABEL = { command: "Command", file: "File", browser: "Browser" } as const;

function Duration({ ms }: { ms: number }) {
  return <span className="tabular-nums">{ms < 1000 ? `${ms} ms` : `${(ms / 1000).toFixed(1)} s`}</span>;
}

function Entry({ entry }: { entry: ActivityEntry }) {
  const Icon = KIND_ICON[entry.kind as keyof typeof KIND_ICON] ?? SquareTerminalIcon;
  const label = KIND_LABEL[entry.kind as keyof typeof KIND_LABEL] ?? "Action";
  const running = entry.status === "running";
  const failed = entry.status === "failed";
  const hasOutput = entry.output !== undefined && entry.output !== "";
  // A command that is still running or failed opens by itself: that is what the user came to see.
  return <details data-slot="activity-entry" data-status={entry.status} open={running || failed || undefined} className="group rounded-lg border bg-card/50 px-2.5 py-1.5">
    <summary className={cn("flex min-w-0 cursor-pointer list-none items-center gap-2 text-xs", !hasOutput && "cursor-default")}>
      <Icon aria-hidden className="size-3.5 shrink-0 text-muted-foreground" />
      <span className="sr-only">{label}: </span>
      <code className="selectable min-w-0 flex-1 truncate font-mono text-foreground">{entry.title}</code>
      <span className="flex shrink-0 items-center gap-1.5 text-muted-foreground">
        {running
          ? <><LoaderCircleIcon aria-hidden className="size-3.5 animate-spin text-info motion-reduce:animate-none" />Running</>
          : failed
            ? <><CircleXIcon aria-hidden className="size-3.5 text-destructive" />{entry.kind === "command" && entry.exitCode ? `Exit ${entry.exitCode}` : "Failed"}</>
            : <><CheckIcon aria-hidden className="size-3.5 text-success" /><Duration ms={entry.durationMs} /></>}
      </span>
    </summary>
    {hasOutput && <pre className="selectable mt-1.5 max-h-60 overflow-auto whitespace-pre-wrap break-words rounded-md bg-background/70 p-2 font-mono text-xs text-muted-foreground">{entry.output}{entry.truncated ? "\n[output cut]" : ""}</pre>}
  </details>;
}

/**
 * The bot's recent commands, file writes and browser actions with their output, newest at the bottom. It follows the
 * newest entry until the user scrolls up. Read-only: the bot's own commands also appear in the Terminal tab when it is
 * open, but this list is kept whether or not the terminal is.
 */
export function ActivityFeed({ botId }: { botId: string }) {
  const { entries, error } = useActivity(botId);
  const scroller = useRef<HTMLDivElement>(null);
  const [following, setFollowing] = useState(true);
  useLayoutEffect(() => {
    const node = scroller.current;
    if (following && node) node.scrollTop = node.scrollHeight;
  }, [entries, following]);
  useEffect(() => { setFollowing(true); }, [botId]);
  const onScroll = () => {
    const node = scroller.current;
    if (node) setFollowing(node.scrollHeight - node.scrollTop - node.clientHeight < 24);
  };

  if (error) return <p role="alert" className="selectable p-2 text-sm text-destructive-foreground">{error}</p>;
  if (!entries) return <p role="status" className="p-2 text-sm text-muted-foreground">Loading…</p>;
  if (entries.length === 0) return <p role="status" className="p-2 text-sm text-muted-foreground">Nothing yet. What the bot runs on its computer shows up here.</p>;
  return <div ref={scroller} onScroll={onScroll} role="log" aria-label="Bot activity" data-slot="activity-feed" className="flex min-h-0 flex-1 flex-col gap-1.5 overflow-auto">
    {entries.map((entry) => <Entry key={entry.id} entry={entry} />)}
  </div>;
}
