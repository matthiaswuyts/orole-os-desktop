import { useEffect, useMemo, useRef, useState, type KeyboardEvent } from "react";
import { MoreHorizontal, Pencil, Plus, Plug, TriangleAlert } from "lucide-react";
import { Chat, type Bot } from "@/mygo";
import { cn } from "@/lib/utils";
import { ago } from "@/features/agents/format";
import { ConfirmDialog } from "@/features/projects/dialogs";
import { useChat } from "@/stores/chat";
import { useHasProvider } from "@/stores/llm";
import { useUI } from "@/stores/ui";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Menu, MenuItem, MenuPopup, MenuSeparator, MenuTrigger } from "@/components/ui/menu";
import { Skeleton } from "@/components/ui/skeleton";
import { BotAvatar } from "./BotAvatar";

const iconButton = "inline-flex size-5 shrink-0 items-center justify-center rounded text-muted-foreground opacity-0 outline-none hover:text-foreground focus-visible:opacity-100 focus-visible:ring-2 focus-visible:ring-sidebar-ring group-hover/row:opacity-100 group-focus-within/row:opacity-100 data-popup-open:opacity-100";
const note = "px-2 py-1 text-muted-foreground text-xs";

/** Relative time from a unix-ms stamp: "just now", "5m", "3h", "2d". */
const since = (at: number) => ago(new Date(at).toISOString()).replace(/ ago$/, "");

/** Keeps relative times fresh without re-rendering every second. */
function useMinuteTick() {
  const [, setTick] = useState(0);
  useEffect(() => {
    const timer = window.setInterval(() => setTick((n) => n + 1), 30_000);
    return () => window.clearInterval(timer);
  }, []);
}

/** Chat mode's sidebar: one contact row per bot, with its last message and a live "responding" dot. */
export function BotRail() {
  const { bots, selectedBotId, available, ready, error, load, startSync } = useChat();
  const hasProvider = useHasProvider();
  const [filter, setFilter] = useState("");
  const [focusId, setFocusId] = useState<string | null>(null);
  const list = useRef<HTMLUListElement>(null);
  useMinuteTick();
  useEffect(() => startSync(), [startSync]);

  const query = filter.trim().toLocaleLowerCase();
  const visible = useMemo(
    () => query ? bots.filter((b) => `${b.name} ${b.description}`.toLocaleLowerCase().includes(query)) : bots,
    [bots, query],
  );
  const tabStop = visible.find((b) => b.id === focusId)?.id ?? visible.find((b) => b.id === selectedBotId)?.id ?? visible[0]?.id;
  const noProvider = hasProvider === false;
  const newBot = () => useChat.getState().openBuilder("new");

  const move = (event: KeyboardEvent<HTMLUListElement>) => {
    const rows = Array.from(list.current?.querySelectorAll<HTMLElement>("[data-bot-row]") ?? []);
    const at = rows.indexOf(event.target as HTMLElement);
    if (at < 0) return;
    const targets: Record<string, number> = { ArrowDown: Math.min(at + 1, rows.length - 1), ArrowUp: Math.max(at - 1, 0), Home: 0, End: rows.length - 1 };
    const to = targets[event.key];
    if (to === undefined) return;
    event.preventDefault();
    rows[to]?.focus();
  };

  let body;
  if (!available) {
    body = <p className={note}>Chat needs the desktop app (no daemon in browser preview).</p>;
  } else if (!ready && error) {
    body = (
      <button type="button" onClick={() => void load()} className="mx-1 mt-1 flex items-start gap-1.5 rounded-md px-1.5 py-1 text-left text-destructive-foreground text-xs outline-none hover:bg-sidebar-accent focus-visible:ring-2 focus-visible:ring-sidebar-ring">
        <TriangleAlert className="mt-px size-3 shrink-0" /><span>Could not load bots: {error}. Click to retry.</span>
      </button>
    );
  } else if (!ready) {
    body = (
      <div role="status" aria-label="Loading bots" className="flex flex-col gap-2 px-2 pt-1">
        {[0, 1, 2].map((n) => (
          <div key={n} className="flex items-center gap-2"><Skeleton className="size-5 rounded-full" /><div className="flex flex-1 flex-col gap-1"><Skeleton className="h-2.5 w-2/3" /><Skeleton className="h-2 w-full" /></div></div>
        ))}
      </div>
    );
  } else if (bots.length === 0 && !noProvider) {
    body = (
      <div className="flex flex-col items-start gap-2 px-2 py-1">
        <p className="text-muted-foreground text-xs">Bots are specialised contacts you can talk to, each with its own memory and tools.</p>
        <Button size="xs" onClick={newBot}><Plus />Create your first bot</Button>
      </div>
    );
  } else if (bots.length > 0) {
    body = visible.length === 0 ? <p className={note}>No bots match “{filter.trim()}”.</p> : (
      <ul ref={list} aria-label="Bots" onKeyDown={move} className="flex flex-col gap-px">
        {visible.map((bot) => <BotRow key={bot.id} bot={bot} selected={bot.id === selectedBotId} tabStop={bot.id === tabStop} onFocus={() => setFocusId(bot.id)} />)}
      </ul>
    );
  }

  return (
    <section aria-label="Bots" className="flex min-h-0 flex-1 flex-col gap-1.5 overflow-auto">
      {available && (
        <Button size="xs" variant="outline" onClick={newBot} disabled={noProvider} title={noProvider ? "Connect a provider before creating a bot" : undefined} className="w-full justify-start">
          <Plus />New bot
        </Button>
      )}
      {available && noProvider && (
        <div role="note" className="mx-1 flex flex-col items-start gap-2 rounded-md border bg-background/40 p-2">
          <p className="text-muted-foreground text-xs">Bots call a model provider directly. Connect OpenRouter or OpenAI to start chatting.</p>
          <Button size="xs" onClick={() => useUI.getState().openSettings("ai")}><Plug />Connect a provider</Button>
        </div>
      )}
      {available && ready && bots.length > 0 && (
        <Input nativeInput size="sm" type="search" aria-label="Filter bots" placeholder="Filter bots" value={filter} onChange={(e) => setFilter(e.target.value)} />
      )}
      {body}
    </section>
  );
}

function BotRow({ bot, selected, tabStop, onFocus }: { bot: Bot; selected: boolean; tabStop: boolean; onFocus: () => void }) {
  const running = useChat((s) => s.running[bot.id] ?? false);
  const menu = useRef<HTMLButtonElement>(null);
  const [confirm, setConfirm] = useState<"clear" | "archive" | null>(null);
  const preview = bot.lastMessage;
  const openMenu = () => menu.current?.click();
  return (
    <li
      onContextMenu={(e) => { e.preventDefault(); openMenu(); }}
      onKeyDown={(e) => { if (e.key === "ContextMenu" || (e.shiftKey && e.key === "F10")) { e.preventDefault(); openMenu(); } }}
      className={cn("group/row relative flex items-center rounded-md pr-1 text-sidebar-foreground hover:bg-sidebar-accent", selected && "bg-sidebar-accent text-sidebar-accent-foreground")}
    >
      <button
        type="button" data-bot-row="" tabIndex={tabStop ? 0 : -1} aria-current={selected ? "true" : undefined}
        onFocus={onFocus} onClick={() => useChat.getState().open(bot.id)}
        className="flex min-h-side-row min-w-0 flex-1 items-center gap-2 rounded-md py-1 pl-2 text-left outline-none focus-visible:ring-2 focus-visible:ring-sidebar-ring"
      >
        <BotAvatar name={bot.name} avatar={bot.avatar} color={bot.color} className="size-6 text-xs" />
        <span className="flex min-w-0 flex-1 flex-col">
          <span className="flex items-center gap-1.5">
            <span className="min-w-0 flex-1 truncate font-medium text-[13px]">{bot.name}</span>
            {running && <span role="img" aria-label="Responding" className="size-1.5 shrink-0 animate-pulse rounded-full bg-info motion-reduce:animate-none" />}
            {preview && <time dateTime={new Date(preview.at).toISOString()} className="shrink-0 text-[11px] text-muted-foreground">{since(preview.at)}</time>}
          </span>
          <span className="truncate text-muted-foreground text-xs">
            {preview ? `${preview.role === "user" ? "You: " : ""}${preview.text}` : "No messages yet"}
          </span>
        </span>
      </button>
      <Menu>
        <MenuTrigger ref={menu} render={<button type="button" tabIndex={-1} aria-label={`Actions for ${bot.name}`} className={cn(iconButton, "absolute top-1 right-1 bg-sidebar-accent")} />}><MoreHorizontal className="size-3.5" /></MenuTrigger>
        <MenuPopup align="start" side="right" className="min-w-44">
          <MenuItem onClick={() => useChat.getState().openBuilder(bot.id)}><Pencil className="size-3.5" />Edit bot</MenuItem>
          <MenuSeparator />
          <MenuItem onClick={() => setConfirm("clear")}>Clear conversation…</MenuItem>
          <MenuItem variant="destructive" onClick={() => setConfirm("archive")}>Archive…</MenuItem>
        </MenuPopup>
      </Menu>
      <ConfirmDialog open={confirm === "clear"} onOpenChange={(o) => setConfirm(o ? "clear" : null)} title={`Clear conversation with ${bot.name}?`} confirmLabel="Clear conversation" onConfirm={() => Chat.clear(bot.id)}>
        <p>This deletes every message in this conversation. The bot’s memory is kept.</p>
      </ConfirmDialog>
      <ConfirmDialog open={confirm === "archive"} onOpenChange={(o) => setConfirm(o ? "archive" : null)} title={`Archive ${bot.name}?`} confirmLabel="Archive bot" onConfirm={() => Chat.archiveBot(bot.id)}>
        <p>{bot.name} leaves your bot list and can no longer be messaged.</p>
      </ConfirmDialog>
    </li>
  );
}
