import { useEffect, useMemo, useRef, useState } from "react";
import { ConversationSearch } from "@/components/assistant-ui/elements/conversation-search";
import type { Message } from "@/mygo";
import { findHits, messageOf } from "./searchHits";

/** Brings a message into view and flashes it, so the match named by the panel can be found in the thread. */
function reveal(root: ParentNode, messageId: string) {
  const el = root.querySelector<HTMLElement>(`[data-message-id="${CSS.escape(messageId)}"]`);
  if (!el) return;
  el.scrollIntoView({ block: "center", behavior: window.matchMedia("(prefers-reduced-motion: reduce)").matches ? "auto" : "smooth" });
  el.animate?.([{ backgroundColor: "color-mix(in oklab, var(--color-amber-400) 25%, transparent)" }, { backgroundColor: "transparent" }], { duration: 1200 });
}

/**
 * Find in this conversation (Ctrl/Cmd+F). It searches the messages that are loaded, so "Load earlier messages" widens
 * it. Enter and Shift+Enter step through the matches, Escape closes it and returns to the composer.
 */
export function ThreadSearch({ messages, scope, onClose }: { messages: readonly Message[]; /** the element holding the rendered thread */ scope: React.RefObject<HTMLElement | null>; onClose: () => void }) {
  const [query, setQuery] = useState("");
  const [active, setActive] = useState(0);
  const root = useRef<HTMLDivElement>(null);
  const hits = useMemo(() => findHits(messages, query), [messages, query]);

  useEffect(() => { root.current?.querySelector("input")?.select(); }, []);

  const go = (index: number) => {
    setActive(index);
    const hit = hits[index];
    if (hit && scope.current) reveal(scope.current, messageOf(hit.id));
  };
  const step = (delta: number) => { if (hits.length > 0) go((Math.min(active, hits.length - 1) + delta + hits.length) % hits.length); };

  return <div ref={root} role="search" data-slot="thread-search" className="absolute top-3 right-4 z-20 flex"
    onKeyDown={(event) => {
      if (event.key === "Escape") { event.preventDefault(); event.stopPropagation(); onClose(); }
      else if (event.key === "Enter") { event.preventDefault(); step(event.shiftKey ? -1 : 1); }
    }}>
    <ConversationSearch className="h-56 w-80 max-w-[calc(100vw-2rem)] rounded-2xl bg-background/95 p-1 shadow-lg backdrop-blur" query={query} hits={hits} activeIndex={active}
      onQueryChange={(next) => {
        setQuery(next);
        setActive(0);
        const first = findHits(messages, next)[0];
        if (first && scope.current) reveal(scope.current, messageOf(first.id));
      }}
      onStep={step} />
  </div>;
}
