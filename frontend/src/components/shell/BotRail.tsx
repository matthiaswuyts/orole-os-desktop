import { useEffect, useMemo, useRef, useState, type KeyboardEvent } from "react";
import { Pencil, Pin, PinOff, Plus, Plug, TriangleAlert } from "lucide-react";
import { Chat, type Bot } from "@/mygo";
import { cn } from "@/lib/utils";
import { ago } from "@/features/agents/format";
import { ConfirmDialog } from "@/features/projects/dialogs";
import { activityOf, useBusyBotIds } from "@/stores/botActivity";
import { useChat } from "@/stores/chat";
import { useHasProvider } from "@/stores/llm";
import { useUI } from "@/stores/ui";
import { Button } from "@/components/ui/button";
import { Menu, MenuItem, MenuPopup, MenuSeparator, MenuTrigger } from "@/components/ui/menu";
import { Skeleton } from "@/components/ui/skeleton";
import { toastManager } from "@/components/ui/toast";
import { Tooltip, TooltipPopup, TooltipTrigger } from "@/components/ui/tooltip";
import { BotAvatar } from "./BotAvatar";

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

/** The sidebar's Bots section: a header with a new-bot button, then one contact row per bot with a status dot. */
export function BotRail() {
  const { bots, selectedBotId, available, ready, error, load, startSync } = useChat();
  // On the Automations view no conversation is on screen, so no bot row is current.
  const chatView = useUI((s) => s.view === "chat");
  const hasProvider = useHasProvider();
  const [focusId, setFocusId] = useState<string | null>(null);
  const list = useRef<HTMLUListElement>(null);
  useMinuteTick();
  useEffect(() => startSync(), [startSync]);

  const visible = useMemo(
    // Pinned bots first; the sort is stable, so each group keeps the list's own order.
    () => [...bots].sort((a, b) => Number(!!b.config.pinned) - Number(!!a.config.pinned)),
    [bots],
  );
  const tabStop = visible.find((b) => b.id === focusId)?.id ?? visible.find((b) => b.id === selectedBotId)?.id ?? visible[0]?.id;
  const noProvider = hasProvider === false;
  // The builder lives in the chat view; from Automations it has to switch there first.
  const newBot = () => { useChat.getState().openBuilder("new"); useUI.getState().setView("chat"); };

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
    body = (
      <ul ref={list} aria-label="Bots" onKeyDown={move} className="flex flex-col gap-px">
        {visible.map((bot) => <BotRow key={bot.id} bot={bot} selected={chatView && bot.id === selectedBotId} tabStop={bot.id === tabStop} onFocus={() => setFocusId(bot.id)} />)}
      </ul>
    );
  }

  return (
    <section aria-label="Bots" className="flex shrink-0 flex-col gap-1">
      <div className="flex h-6 shrink-0 items-center gap-1 pr-0.5 pl-2">
        <h2 className="flex-1 font-medium text-muted-foreground text-xs">Bots</h2>
        {available && !(ready && bots.length === 0 && !noProvider) && (noProvider
          ? <Tooltip><TooltipTrigger render={<span className="inline-flex" />}><Button size="icon-xs" variant="ghost" disabled aria-label="New bot"><Plus /></Button></TooltipTrigger><TooltipPopup>Connect a provider before creating a bot</TooltipPopup></Tooltip>
          : <Tooltip><TooltipTrigger render={<Button size="icon-xs" variant="ghost" onClick={newBot} aria-label="New bot" />}><Plus /></TooltipTrigger><TooltipPopup>New bot</TooltipPopup></Tooltip>)}
      </div>
      {available && noProvider && (
        <div role="note" className="mx-1 flex flex-col items-start gap-2 rounded-md border bg-background/40 p-2">
          <p className="text-muted-foreground text-xs">Bots call a model provider directly. Connect OpenRouter or OpenAI to start chatting.</p>
          <Button size="xs" onClick={() => useUI.getState().openSettings("ai")}><Plug />Connect a provider</Button>
        </div>
      )}
      {body}
    </section>
  );
}

function BotRow({ bot, selected, tabStop, onFocus }: { bot: Bot; selected: boolean; tabStop: boolean; onFocus: () => void }) {
  const responding = useChat((s) => s.running[bot.id] ?? false);
  const hasTask = useBusyBotIds().includes(bot.id);
  const activity = activityOf(responding, hasTask);
  const running = activity !== "idle";
  const menu = useRef<HTMLButtonElement>(null);
  const [confirm, setConfirm] = useState<"clear" | "archive" | null>(null);
  const preview = bot.lastMessage;
  const awaiting = !running && (preview?.awaiting ?? false);
  const openMenu = () => menu.current?.click();
  const pinned = !!bot.config.pinned;
  const togglePin = () => {
    Chat.pinBot({ id: bot.id, pinned: !pinned }).catch((cause: unknown) => {
      toastManager.add({ title: pinned ? "Could not unpin the bot" : "Could not pin the bot", description: cause instanceof Error ? cause.message.replace(/^[a-z_]+: /, "") : String(cause), type: "error" });
    });
  };
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
        <span className="relative shrink-0">
          <BotAvatar name={bot.name} avatar={bot.avatar} className="size-6" />
          {/* Nothing is drawn while the bot is idle: a dot is only ever a claim that something is happening or waiting. */}
          {(running || awaiting) && <span aria-hidden="true" data-status={running ? activity : "waiting"} className={cn(
            "absolute -top-0.5 -right-0.5 size-2 rounded-full ring-2 ring-sidebar",
            activity === "responding" ? "animate-pulse bg-primary motion-reduce:animate-none" : activity === "working" ? "animate-pulse bg-info motion-reduce:animate-none" : "bg-warning",
          )} />}
        </span>
        <span className="flex min-w-0 flex-1 flex-col">
          <span className="flex items-center gap-1.5">
            <span className="min-w-0 flex-1 truncate font-medium text-[13px]">{bot.name}</span>
            {pinned && <Pin aria-label="Pinned" className="size-3 shrink-0 -rotate-45 fill-current text-muted-foreground" />}
            {preview && <time dateTime={new Date(preview.at).toISOString()} className="shrink-0 text-[11px] text-muted-foreground">{since(preview.at)}</time>}
          </span>
          <span className="truncate text-muted-foreground text-xs">
            {preview ? (preview.text ? `${preview.role === "user" ? "You: " : ""}${preview.text}` : "Starting…") : "No messages yet"}
          </span>
        </span>
        {(running || awaiting) && <span className="sr-only">{activity === "responding" ? "Responding" : activity === "working" ? "Running a code task" : "Waiting for your answer"}</span>}
      </button>
      <Menu>
        <MenuTrigger ref={menu} render={<button type="button" tabIndex={-1} aria-hidden="true" aria-label={`Actions for ${bot.name}`} className="pointer-events-none absolute top-1 right-1 size-5 opacity-0" />} />
        <MenuPopup align="start" side="right" className="min-w-44">
          <MenuItem onClick={togglePin}>{pinned ? <PinOff className="size-3.5" /> : <Pin className="size-3.5" />}{pinned ? "Unpin" : "Pin to top"}</MenuItem>
          <MenuItem onClick={() => { useChat.getState().open(bot.id); useChat.getState().setSettingsOpen(true); }}><Pencil className="size-3.5" />Bot settings</MenuItem>
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
