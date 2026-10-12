import { useState, useSyncExternalStore, type ReactNode } from "react";
import { Command as CommandIcon, Plug, Sparkles, SquarePen, Zap } from "lucide-react";
import { Widget } from "@/components/widgets/Widget";
import type { WidgetProps } from "@/components/widgets/registry";
import { getCommands, getRecent, pushRecent, registryVersion, subscribeRegistry, type Command } from "@/lib/commands";

/** Always offered first, when registered; recently used palette commands fill the rest. */
const PINNED = ["task.new", "automation.new", "nav.review", "settings.mcp", "settings.skills"];
const MAX = 8;
/** For built-in commands registered without an icon. */
const FALLBACK_ICON: Record<string, ReactNode> = { "task.new": <SquarePen />, "settings.mcp": <Plug />, "settings.skills": <Sparkles /> };
const shortTitle = (title: string) => title.replace(/…$/, "").replace(/^Go to /, "").replace(/^Open (.)/, (_, c: string) => c.toUpperCase());

/** One-click palette commands: the essentials plus what you ran recently from search. */
export function QuickActionsWidget({ onRemove }: WidgetProps) {
  // Re-renders when commands are (un)registered; the list itself is a cheap lookup.
  useSyncExternalStore(subscribeRegistry, registryVersion, registryVersion);
  const [error, setError] = useState<string | null>(null);
  const all = getCommands();
  const actions = [...PINNED, ...getRecent().filter((id) => !PINNED.includes(id))]
    .map((id) => all.find((c) => c.id === id)).filter((c): c is Command => !!c).slice(0, MAX);
  const run = async (command: Command) => {
    setError(null);
    try { await command.run(); pushRecent(command.id); } catch (e) { setError(e instanceof Error ? e.message : String(e)); }
  };
  return (
    <Widget title="Quick actions" icon={<Zap />} onRemove={onRemove}>
      <ul className="-m-1.5 grid grid-cols-1 gap-0.5">
        {actions.map((command) => (
          <li key={command.id}>
            <button type="button" onClick={() => void run(command)} title={command.title}
              className="flex w-full items-center gap-2.5 rounded-md px-2 py-1.5 text-left text-[13px] outline-none hover:bg-accent focus-visible:ring-2 focus-visible:ring-ring">
              <span className="inline-flex size-6 shrink-0 items-center justify-center rounded-md border bg-card text-muted-foreground [&_svg]:size-3.5" aria-hidden>{command.icon ?? FALLBACK_ICON[command.id] ?? <CommandIcon />}</span>
              <span className="truncate">{shortTitle(command.title)}</span>
            </button>
          </li>
        ))}
      </ul>
      {actions.length === 0 && <p className="text-muted-foreground text-xs">Commands are still loading.</p>}
      {error && <p role="alert" className="mt-2 text-destructive-foreground text-xs">{error}</p>}
      <p className="mt-2 text-muted-foreground text-xs">More in search (Ctrl+K).</p>
    </Widget>
  );
}
