/*
 * Props follow the assistant-ui conversation-map element (https://www.assistant-ui.com/elements/conversation-map):
 * a rail with one tick per turn, the turn being read drawn solid, the turns on screen deeper, a hover preview and a click
 * that jumps there. The markup, keyboard handling and styling below are written for Orole OS, not copied from upstream.
 */
"use client";

import { useRef, useState, type ComponentProps, type KeyboardEvent } from "react";
import { cn } from "@/lib/utils";
import { paper } from "./surfaces";

export interface ConversationMapEntry {
  id: string;
  /** The card's heading: usually the first line of the user's message. */
  title: string;
  /** The card's body: a short excerpt of the reply. */
  preview?: string;
  /** How much happened in the turn (any unit); longer turns get longer ticks once the rail is pointed at. */
  length?: number;
}

const MIN_TICK = 10;
const MAX_TICK = 28;

/** Tick width in px: 10 for a one-line turn, growing with the turn's length up to 28. */
function tickWidth(length: number | undefined, longest: number): number {
  if (!length || longest <= 1) return MIN_TICK;
  return Math.round(MIN_TICK + (Math.min(length, longest) / longest) * (MAX_TICK - MIN_TICK));
}

/**
 * A rail of the whole conversation. Ticks are buttons: only the current one is a tab stop, and Up/Down/Home/End move between them.
 * Hide it where there is no gutter beside the message column (`className="hidden @5xl/feed:flex"` for example).
 */
export function ConversationMap({ entries, activeId, visibleIds, onSelect, side = "right", className, ...props }: Omit<ComponentProps<"nav">, "children" | "onSelect"> & {
  entries: readonly ConversationMapEntry[];
  /** The turn being read, drawn as the one solid tick. */
  activeId?: string;
  /** Every turn the viewport holds, drawn deeper than the rest and fanned out by length once the rail is pointed at. */
  visibleIds?: readonly string[];
  onSelect?: (id: string) => void;
  /** Which side of the rail the preview card opens on. */
  side?: "left" | "right";
}) {
  const list = useRef<HTMLUListElement>(null);
  // The preview card exists only for the tick being pointed at or focused, so a long conversation does not mount one per turn.
  const [pointed, setPointed] = useState<string | null>(null);
  const visible = new Set(visibleIds);
  const longest = entries.reduce((max, entry) => Math.max(max, entry.length ?? 0), 0);
  const current = entries.some((entry) => entry.id === activeId) ? activeId : entries[0]?.id;

  const onKeyDown = (event: KeyboardEvent<HTMLUListElement>) => {
    const ticks = Array.from(list.current?.querySelectorAll<HTMLButtonElement>("button") ?? []);
    const at = ticks.indexOf(document.activeElement as HTMLButtonElement);
    const next = event.key === "ArrowDown" ? at + 1 : event.key === "ArrowUp" ? at - 1 : event.key === "Home" ? 0 : event.key === "End" ? ticks.length - 1 : null;
    if (next === null || at < 0) return;
    event.preventDefault();
    ticks[Math.min(Math.max(next, 0), ticks.length - 1)]?.focus();
  };

  return <nav data-slot="conversation-map" aria-label="Conversation map" className={cn("group/rail flex flex-col py-1", className)} {...props}>
    <ul ref={list} onKeyDown={onKeyDown} className="m-0 flex min-h-0 list-none flex-col overflow-y-auto p-0 [scrollbar-width:none] [&::-webkit-scrollbar]:hidden">
      {entries.map((entry, index) => {
        const active = entry.id === activeId;
        const onScreen = visible.has(entry.id);
        return <li key={entry.id} className="group/tick relative" onMouseEnter={() => setPointed(entry.id)} onMouseLeave={() => setPointed((id) => id === entry.id ? null : id)}
          onFocus={(event) => { if (event.target.matches(":focus-visible")) setPointed(entry.id); }} onBlur={() => setPointed((id) => id === entry.id ? null : id)}>
          <button type="button" tabIndex={entry.id === current ? 0 : -1} aria-current={active ? "location" : undefined} aria-label={`Turn ${index + 1}: ${entry.title}`}
            onClick={() => onSelect?.(entry.id)}
            className={cn("flex h-3 w-8 items-center rounded-sm outline-none focus-visible:ring-2 focus-visible:ring-ring", side === "left" ? "justify-end" : "justify-start")}>
            <span aria-hidden style={{ "--tick": `${tickWidth(entry.length, longest)}px` } as React.CSSProperties}
              className={cn("h-0.5 w-2.5 rounded-full transition-[width,background-color] duration-150 group-hover/rail:w-(--tick) group-focus-within/rail:w-(--tick) motion-reduce:transition-none",
                active ? "bg-foreground" : onScreen ? "bg-foreground/50" : "bg-foreground/20 group-hover/tick:bg-foreground/40")} />
          </button>
          {pointed === entry.id && <div aria-hidden className={cn(paper, "pointer-events-none absolute top-1/2 z-20 flex w-64 -translate-y-1/2 flex-col gap-1 p-3 text-xs shadow-lg/5", side === "left" ? "right-full mr-2" : "left-full ml-2")}>
            <p className="line-clamp-2 font-medium text-foreground">{entry.title}</p>
            {entry.preview && <p className="line-clamp-4 text-muted-foreground">{entry.preview}</p>}
          </div>}
        </li>;
      })}
    </ul>
  </nav>;
}
